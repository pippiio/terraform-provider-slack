package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

//Validate Types test

func TestMessageResource_ValidTypes(t *testing.T) {
	allowedTypes := []string{"channel", "user", "multiple_users"}

	for _, typ := range allowedTypes {
		t.Run(typ, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`resource "slack_message" "this" {type = "%s"}`, typ),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("resource.slack_message.this", "type", typ),
						),
					},
				},
			})
		})
	}
}

func TestMessageResource_InvalidTypeFails(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `resource "slack_message" "this" {type = "invalid_type"}`,
				ExpectError: regexp.MustCompile(`expected type to be one of \[channel user multiple_users\]`),
			},
		},
	})
}

// Validate that message content must be specified

func TestMessageResource_NullMessage(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "slack_message" "this" {
					type = "channel" 
					slack_ID = "C123456789"
				}`,
				ExpectError: regexp.MustCompile(`Message Field must be set`),
			},
		},
	})
}

// Verified that recipients is specified only by slack id

func TestMessageResource_WrongSlackID(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "slack_message" "this" {
					type = "channel" 
					message = "Test Message"
					slack_ID = "Username"
				}`,
				ExpectError: regexp.MustCompile(`slack_ID must be in format \[C1A2B3C4D U1A2B3C4D\]`),
			},
		},
	})
}

// Verified that message is sent with specified content to specified recipients on (and only on) resource creation

func TestMessageResource_Created(t *testing.T) {

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "slack_message" "this"{
				  type = "channel"
				  message = "Test Message"
				  slack_ID = "C123456789"
				}`,

				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("resource.slack_message.this", "type", "channel"),
					resource.TestCheckResourceAttr("resource.slack_message.this", "message", "Test Message"),
					resource.TestCheckResourceAttr("resource.slack_message.this", "slack_ID", "C123456789"),
				),
			},
		},
	})
}

// Verified that message is updated when content is changed

func TestMessageResource_Updated(t *testing.T) {

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "slack_message" "this"{
				  type = "channel"
				  message = "Test Message Created"
				  slack_ID = "C123456789"
				}`,

				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("resource.slack_message.this", "type", "channel"),
					resource.TestCheckResourceAttr("resource.slack_message.this", "message", "Test Message Created"),
					resource.TestCheckResourceAttr("resource.slack_message.this", "slack_ID", "C123456789"),
				),
			},
			{
				Config: `
				resource "slack_message" "this"{
				  type = "channel"
				  message = "Test Message Updated"
				  slack_ID = "C123456789"
				}`,

				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("resource.slack_message.this", "type", "channel"),
					resource.TestCheckResourceAttr("resource.slack_message.this", "message", "Test Message Updated"),
					resource.TestCheckResourceAttr("resource.slack_message.this", "slack_ID", "C123456789"),
				),
			},
		},
	})
}

// verified that resource is destroyed when other attributes than content is changed

//All of the other tests does that by default, when test is ended
