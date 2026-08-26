package slackclient

// Story: Slack user object
//
// Input:  a users.info or users.lookupByEmail response body.
// Process:
//   1. Decode the {"ok":..., "user":{...}} envelope.
//   2. Map the user object and its nested profile onto Go types, preserving the
//      difference between "Slack omitted this field" and "Slack sent an empty value".
// Output: a *User the provider layer maps onto the Terraform schema.
//
// Dependencies: encoding/json only.
// Side effects: none.
//
// Why the pointers: Slack omits fields rather than sending zero values -- profile.email
// is absent entirely unless the token holds users:read.email, and bot accounts carry no
// title or phone. A plain string would render those as "" in Terraform state, which
// reads as "this user has an empty title" rather than "Slack did not tell us". Pointers
// let the provider emit a null attribute instead, which is what AC-2c requires.
//
// Fields Slack always returns (id, team_id, name) stay plain strings.

import (
	"bytes"
	"encoding/json"
)

// userResponse is the envelope returned by users.info and users.lookupByEmail.
type userResponse struct {
	User User `json:"user"`
}

// User is a Slack user object.
//
// Deliberately omitted, per the track's Non-Goals:
//   - enterprise_user (Enterprise Grid only)
//   - locale          (requires include_locale=true)
type User struct {
	ID     string `json:"id"`
	TeamID string `json:"team_id"`
	Name   string `json:"name"`

	Deleted  *bool   `json:"deleted"`
	Color    *string `json:"color"`
	RealName *string `json:"real_name"`
	TZ       *string `json:"tz"`
	TZLabel  *string `json:"tz_label"`
	TZOffset *int64  `json:"tz_offset"`

	IsAdmin           *bool `json:"is_admin"`
	IsOwner           *bool `json:"is_owner"`
	IsPrimaryOwner    *bool `json:"is_primary_owner"`
	IsRestricted      *bool `json:"is_restricted"`
	IsUltraRestricted *bool `json:"is_ultra_restricted"`
	IsBot             *bool `json:"is_bot"`
	IsAppUser         *bool `json:"is_app_user"`
	IsEmailConfirmed  *bool `json:"is_email_confirmed"`
	Has2FA            *bool `json:"has_2fa"`

	Updated *int64 `json:"updated"`

	Profile Profile `json:"profile"`
}

// Profile is the nested profile object on a Slack user.
type Profile struct {
	RealName              *string `json:"real_name"`
	RealNameNormalized    *string `json:"real_name_normalized"`
	DisplayName           *string `json:"display_name"`
	DisplayNameNormalized *string `json:"display_name_normalized"`
	FirstName             *string `json:"first_name"`
	LastName              *string `json:"last_name"`

	// Email is absent unless the token holds the users:read.email scope.
	Email *string `json:"email"`

	Title *string `json:"title"`
	Phone *string `json:"phone"`
	Skype *string `json:"skype"`
	Team  *string `json:"team"`

	StatusText       *string `json:"status_text"`
	StatusEmoji      *string `json:"status_emoji"`
	StatusExpiration *int64  `json:"status_expiration"`

	AvatarHash    *string `json:"avatar_hash"`
	Image24       *string `json:"image_24"`
	Image32       *string `json:"image_32"`
	Image48       *string `json:"image_48"`
	Image72       *string `json:"image_72"`
	Image192      *string `json:"image_192"`
	Image512      *string `json:"image_512"`
	Image1024     *string `json:"image_1024"`
	ImageOriginal *string `json:"image_original"`
	IsCustomImage *bool   `json:"is_custom_image"`

	BotID    *string `json:"bot_id"`
	APIAppID *string `json:"api_app_id"`

	// Fields holds tenant-defined custom profile fields, keyed by Slack's field ID
	// (e.g. "Xf0123456"). See ProfileFields for the decoding quirk.
	Fields ProfileFields `json:"fields"`
}

// ProfileField is one custom profile field value.
type ProfileField struct {
	Value *string `json:"value"`
	Alt   *string `json:"alt"`
}

// ProfileFields maps a Slack custom-profile-field ID to its value.
//
// Slack is inconsistent about how it represents this: an object when the user has custom
// fields set, but an empty JSON *array* when they do not. Decoding straight into a
// map[string]ProfileField therefore fails for every user without custom fields, which is
// why this type exists.
//
// The three states are kept distinct:
//   - key absent, or null -> nil          ("Slack told us nothing")
//   - []                  -> empty map    ("this user has no custom fields")
//   - {...}               -> populated map
type ProfileFields map[string]ProfileField

func (f *ProfileFields) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*f = nil
		return nil
	}

	// Slack's "no custom fields" representation. Treat any array as empty rather than
	// erroring -- the array form never carries entries.
	if trimmed[0] == '[' {
		*f = ProfileFields{}
		return nil
	}

	var m map[string]ProfileField
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return err
	}
	*f = m
	return nil
}
