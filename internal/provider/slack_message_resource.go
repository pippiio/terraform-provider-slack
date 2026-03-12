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
				Required: true,
			},
			"slack_ids": schema.SetAttribute{
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
							Computed: true,
						},
						"channel": schema.StringAttribute{
							Computed: true,
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

	client, ok := req.ProviderData.(*slackclient.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *hashicups.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
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

		apiresp, err := r.client.SendMessage(plan.Message.ValueString(), v.(types.String).ValueString())
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
		if err != nil || apiresp.Err == "thread_not_found" {
			continue
		}

		channelValue := types.StringValue(apiresp.Channel)
		if apiresp.Channel == "" {
			channelValue = types.StringValue(channel)
		}

		obj, diagsObj := types.ObjectValue(map[string]attr.Type{
			"ts":      types.StringType,
			"channel": types.StringType,
		}, map[string]attr.Value{
			"ts":      types.StringValue(apiresp.Messages[0].Ts),
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
}

func (r *messageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}
