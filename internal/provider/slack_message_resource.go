package provider

import (
	"context"
	"fmt"
	"terraform-provider-slack/internal/slackclient"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// msgMapAttrTypes is the shape of one msg_map entry.
var msgMapAttrTypes = map[string]attr.Type{
	"ts":      types.StringType,
	"channel": types.StringType,
}

var (
	_ resource.Resource              = &messageResource{}
	_ resource.ResourceWithConfigure = &messageResource{}
)

func NewMessageResource() resource.Resource {
	return &messageResource{}
}

type messageResource struct {
	client *slackclient.Client
}

type messageResourceModel struct {
	Message     types.String `tfsdk:"message"`
	Slack_IDs   types.Set    `tfsdk:"slack_ids"`
	LastUpdated types.String `tfsdk:"last_updated"`
	Msg_map     types.Map    `tfsdk:"msg_map"`
}

func (r *messageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_message"
}

func (r *messageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"message": schema.StringAttribute{
				Description: "Slack message to be sent",
				Required:    true,
			},
			"slack_ids": schema.SetAttribute{
				Description: "Set of slackids to send the message to",
				ElementType: types.StringType,
				Required:    true,
			},
			"last_updated": schema.StringAttribute{
				Computed: true,
			},
			"msg_map": schema.MapNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"ts": schema.StringAttribute{
							Description: "Time stamp of the message",
							Computed:    true,
						},
						"channel": schema.StringAttribute{
							Description: "Channel id of the message",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (r *messageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clients := providerClientsFrom(req.ProviderData, "Resource", &resp.Diagnostics)
	if clients == nil {
		return
	}

	r.client = clients.Bot
}

func (r *messageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan messageResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	msgMap := make(map[string]attr.Value)

	for _, v := range plan.Slack_IDs.Elements() {

		apiresp, err := r.client.SendMessage(v.(types.String).ValueString(), plan.Message.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error send message",
				"Could not send message, unexpected error: "+err.Error(),
			)
			return
		}

		obj, diagsObj := types.ObjectValue(map[string]attr.Type{
			"ts":      types.StringType,
			"channel": types.StringType,
		}, map[string]attr.Value{
			"ts":      types.StringValue(apiresp.Ts),
			"channel": types.StringValue(apiresp.Channel),
		})
		resp.Diagnostics.Append(diagsObj...)
		if resp.Diagnostics.HasError() {
			return
		}

		msgMap[v.(types.String).ValueString()] = obj
	}

	var diagsMap diag.Diagnostics
	plan.Msg_map, diagsMap = types.MapValue(types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"ts":      types.StringType,
			"channel": types.StringType,
		},
	}, msgMap)
	resp.Diagnostics.Append(diagsMap...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.LastUpdated = types.StringValue(time.Now().Format(time.RFC850))

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func (r *messageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state messageResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	msgMap := make(map[string]attr.Value)

	for k, v := range state.Msg_map.Elements() {

		channel := v.(types.Object).Attributes()["channel"].(types.String).ValueString()
		ts := v.(types.Object).Attributes()["ts"].(types.String).ValueString()
		apiresp, err := r.client.ReadMessage(channel, ts)

		switch classifyReadError(err) {
		case readOutcomeDrop:
			// The message is confirmed gone from Slack. Dropping it here surfaces the
			// drift to Terraform, which recreates it on the next apply.
			continue
		case readOutcomeFail:
			// We could not determine whether the message still exists. Leaving state
			// untouched and failing is the safe outcome -- silently dropping the entry
			// would destroy the record of a message that probably still exists.
			resp.Diagnostics.AddError(
				"Unable to read Slack message",
				fmt.Sprintf(
					"Could not read the message for %s (channel: %s, ts: %s): %s\n\n"+
						"State has been left unchanged. Resolve the error above and run "+
						"terraform refresh again.",
					k, channel, ts, err,
				),
			)
			return
		}

		channelValue := types.StringValue(apiresp.Channel)
		tsValue := types.StringValue(ts)
		if apiresp.Channel == "" {
			channelValue = types.StringValue(channel)
		}
		if len(apiresp.Messages) > 0 && apiresp.Messages[0].Ts != "" {
			tsValue = types.StringValue(apiresp.Messages[0].Ts)
		}
		obj, diagsObj := types.ObjectValue(map[string]attr.Type{
			"ts":      types.StringType,
			"channel": types.StringType,
		}, map[string]attr.Value{
			"ts":      tsValue,
			"channel": channelValue,
		})
		resp.Diagnostics.Append(diagsObj...)
		if resp.Diagnostics.HasError() {
			return
		}

		msgMap[k] = obj
	}
	state.LastUpdated = types.StringValue(time.Now().Format(time.RFC850))

	if len(msgMap) <= 0 {
		resp.State.RemoveResource(ctx)
		return
	}

	var diagsMap diag.Diagnostics
	state.Msg_map, diagsMap = types.MapValue(types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"ts":      types.StringType,
			"channel": types.StringType,
		},
	}, msgMap)
	resp.Diagnostics.Append(diagsMap...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func (r *messageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan messageResourceModel
	var state messageResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	planIDs := make(map[string]bool)
	for _, v := range plan.Slack_IDs.Elements() {
		planIDs[v.(types.String).ValueString()] = true
	}

	// msgMap starts as what Slack holds right now and is mutated only as calls
	// succeed. Reposting deletes before it posts, so an Update that fails half way
	// must not leave state holding a ts for a message that is already gone.
	msgMap := make(map[string]attr.Value, len(state.Msg_map.Elements()))
	for id, v := range state.Msg_map.Elements() {
		msgMap[id] = v
	}

	// Recipients dropped from the plan lose their message.
	for slackID, v := range state.Msg_map.Elements() {
		if planIDs[slackID] {
			continue
		}
		// This error used to be discarded, which orphaned the message: it stayed in
		// the workspace while the entry vanished from state, tracked by nothing.
		resp.Diagnostics.Append(r.deleteStoredMessage(slackID, v)...)
		if resp.Diagnostics.HasError() {
			r.writeMessageState(ctx, resp, state, msgMap)
			return
		}
		delete(msgMap, slackID)
	}

	textChanged := plan.Message.ValueString() != state.Message.ValueString()

	for slackID := range planIDs {
		existing, exists := msgMap[slackID]

		if exists && !textChanged {
			continue
		}

		if exists {
			// Repost rather than edit. chat.update leaves the message where it sits in
			// the conversation, so a recipient who has scrolled past never sees the new
			// text -- and it fails outright with message_not_found once the original is
			// gone, wedging every later apply. Deleting first makes the replacement the
			// most recent message, and a delete that reports the message already gone
			// counts as success, so the wedged case repairs itself.
			resp.Diagnostics.Append(r.deleteStoredMessage(slackID, existing)...)
			if resp.Diagnostics.HasError() {
				r.writeMessageState(ctx, resp, state, msgMap)
				return
			}
			delete(msgMap, slackID)
		}

		apiResp, err := r.client.SendMessage(slackID, plan.Message.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error sending Slack message",
				fmt.Sprintf("Slack ID: %s, Error: %s", slackID, err.Error()),
			)
			// Any message this loop already deleted is gone for good. Recording that is
			// what lets the next apply post a replacement instead of editing a ghost.
			r.writeMessageState(ctx, resp, state, msgMap)
			return
		}

		obj, diagsObj := types.ObjectValue(msgMapAttrTypes, map[string]attr.Value{
			"ts":      types.StringValue(apiResp.Ts),
			"channel": types.StringValue(apiResp.Channel),
		})
		resp.Diagnostics.Append(diagsObj...)
		if resp.Diagnostics.HasError() {
			return
		}
		msgMap[slackID] = obj
	}

	r.writeMessageState(ctx, resp, plan, msgMap)
}

// writeMessageState stores model with the given msg_map. Every exit from Update goes
// through it, including the failing ones.
//
// On failure it is called with the prior state, so the recorded message text stays the
// one recipients can actually see. Claiming the new text after a partial repost would
// leave a recipient holding the old message with nothing to make a later apply revisit
// it -- permanent, silent drift. Repeating a repost is recoverable; drift is not.
func (r *messageResource) writeMessageState(ctx context.Context, resp *resource.UpdateResponse, model messageResourceModel, msgMap map[string]attr.Value) {
	m, diags := types.MapValue(types.ObjectType{AttrTypes: msgMapAttrTypes}, msgMap)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	model.Msg_map = m
	model.LastUpdated = types.StringValue(time.Now().Format(time.RFC850))
	resp.Diagnostics.Append(resp.State.Set(ctx, model)...)
}

func (r *messageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state messageResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	for slackID, v := range state.Msg_map.Elements() {
		resp.Diagnostics.Append(r.deleteStoredMessage(slackID, v)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
}

// deleteStoredMessage deletes the message recorded in one msg_map entry.
//
// Two details are load-bearing. It targets the stored channel rather than the map
// key: the key is the Slack ID the config named -- for a DM, a user ID -- while the
// message lives in the conversation Slack opened for it. And a Slack code that
// positively confirms the message is already gone counts as success, so a message
// somebody deleted by hand cannot wedge every later apply and destroy.
func (r *messageResource) deleteStoredMessage(slackID string, entry attr.Value) diag.Diagnostics {
	var diags diag.Diagnostics

	attrs := entry.(types.Object).Attributes()
	ts := attrs["ts"].(types.String).ValueString()
	channel := attrs["channel"].(types.String).ValueString()

	if err := r.client.DeleteMessage(channel, ts); err != nil && !messageGoneCodes[slackclient.ErrorCode(err)] {
		diags.AddError(
			"Error Deleting Slack Message",
			fmt.Sprintf(
				"Could not delete the message for %s (channel: %s, ts: %s): %s\n\n"+
					"State has been left unchanged. The message may still exist in Slack.",
				slackID, channel, ts, err,
			),
		)
	}

	return diags
}
