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
		id := v.(types.String).ValueString()
		planIDs[id] = true
	}

	stateIDs := make(map[string]bool)
	for k := range state.Msg_map.Elements() {
		stateIDs[k] = true
	}

	for slackID := range stateIDs {
		if planIDs[slackID] {
			continue
		}
		// This error used to be discarded, which orphaned the message: it stayed in
		// the workspace while the entry vanished from state, tracked by nothing.
		resp.Diagnostics.Append(r.deleteStoredMessage(slackID, state.Msg_map.Elements()[slackID])...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	msgMap := make(map[string]attr.Value)

	for slackID := range planIDs {
		var ts, channel string
		exists := false

		if s, ok := state.Msg_map.Elements()[slackID]; ok {
			obj := s.(types.Object)
			attrs := obj.Attributes()
			ts = attrs["ts"].(types.String).ValueString()
			channel = attrs["channel"].(types.String).ValueString()
			exists = true
		}

		var apiResp *slackclient.Response
		var err error

		if !exists {

			apiResp, err = r.client.SendMessage(slackID, plan.Message.ValueString())

		} else if plan.Message.ValueString() != state.Message.ValueString() {

			apiResp, err = r.client.UpdateMessage(channel, ts, plan.Message.ValueString())

		}

		if err != nil {
			resp.Diagnostics.AddError(
				"Error sending/updating message",
				fmt.Sprintf("Slack ID: %s, Error: %s", slackID, err.Error()),
			)
			return
		}

		if apiResp != nil {
			if apiResp.Ts != "" {
				ts = apiResp.Ts
			}
			if len(apiResp.Messages) > 0 {
				ts = apiResp.Messages[0].Ts
			}
			if apiResp.Channel != "" {
				channel = apiResp.Channel
			}
		}
		msgMap[slackID], _ = types.ObjectValue(
			map[string]attr.Type{
				"ts":      types.StringType,
				"channel": types.StringType,
			},
			map[string]attr.Value{
				"ts":      types.StringValue(ts),
				"channel": types.StringValue(channel),
			},
		)
	}

	plan.Msg_map, diags = types.MapValue(types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"ts":      types.StringType,
			"channel": types.StringType,
		},
	}, msgMap)
	resp.Diagnostics.Append(diags...)
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
