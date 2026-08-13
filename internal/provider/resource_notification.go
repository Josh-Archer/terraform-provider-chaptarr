package provider

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Josh-Archer/terraform-provider-chaptarr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ResourceWithImportState = &notificationResource{}

type notificationResource struct{ client *client.Client }
type notificationModel struct {
	ID                         types.String `tfsdk:"id"`
	Name                       types.String `tfsdk:"name"`
	ImplementationName         types.String `tfsdk:"implementation_name"`
	Implementation             types.String `tfsdk:"implementation"`
	ConfigContract             types.String `tfsdk:"config_contract"`
	Enable                     types.Bool   `tfsdk:"enable"`
	TestOnApply                types.Bool   `tfsdk:"test_on_apply"`
	Tags                       types.Set    `tfsdk:"tags"`
	FieldValuesJSON            types.String `tfsdk:"field_values_json"`
	FieldValuesSHA256          types.String `tfsdk:"field_values_sha256"`
	SecretFields               types.Map    `tfsdk:"secret_fields"`
	ProtectedFieldNames        types.Set    `tfsdk:"protected_field_names"`
	OnGrab                     types.Bool   `tfsdk:"on_grab"`
	OnReleaseImport            types.Bool   `tfsdk:"on_release_import"`
	OnUpgrade                  types.Bool   `tfsdk:"on_upgrade"`
	OnRename                   types.Bool   `tfsdk:"on_rename"`
	OnAuthorAdded              types.Bool   `tfsdk:"on_author_added"`
	OnBookAdded                types.Bool   `tfsdk:"on_book_added"`
	OnAuthorDelete             types.Bool   `tfsdk:"on_author_delete"`
	OnBookDelete               types.Bool   `tfsdk:"on_book_delete"`
	OnBookFileDelete           types.Bool   `tfsdk:"on_book_file_delete"`
	OnBookFileDeleteForUpgrade types.Bool   `tfsdk:"on_book_file_delete_for_upgrade"`
	OnHealthIssue              types.Bool   `tfsdk:"on_health_issue"`
	IncludeHealthWarnings      types.Bool   `tfsdk:"include_health_warnings"`
	OnDownloadFailure          types.Bool   `tfsdk:"on_download_failure"`
	OnImportFailure            types.Bool   `tfsdk:"on_import_failure"`
	OnBookRetag                types.Bool   `tfsdk:"on_book_retag"`
	OnApplicationUpdate        types.Bool   `tfsdk:"on_application_update"`
	SupportedEvents            types.Set    `tfsdk:"supported_events"`
}
type notificationAPI struct {
	integrationBaseAPI
	OnGrab                             bool `json:"onGrab"`
	OnReleaseImport                    bool `json:"onReleaseImport"`
	OnUpgrade                          bool `json:"onUpgrade"`
	OnRename                           bool `json:"onRename"`
	OnAuthorAdded                      bool `json:"onAuthorAdded"`
	OnBookAdded                        bool `json:"onBookAdded"`
	OnAuthorDelete                     bool `json:"onAuthorDelete"`
	OnBookDelete                       bool `json:"onBookDelete"`
	OnBookFileDelete                   bool `json:"onBookFileDelete"`
	OnBookFileDeleteForUpgrade         bool `json:"onBookFileDeleteForUpgrade"`
	OnHealthIssue                      bool `json:"onHealthIssue"`
	IncludeHealthWarnings              bool `json:"includeHealthWarnings"`
	OnDownloadFailure                  bool `json:"onDownloadFailure"`
	OnImportFailure                    bool `json:"onImportFailure"`
	OnBookRetag                        bool `json:"onBookRetag"`
	OnApplicationUpdate                bool `json:"onApplicationUpdate"`
	SupportsOnGrab                     bool `json:"supportsOnGrab"`
	SupportsOnReleaseImport            bool `json:"supportsOnReleaseImport"`
	SupportsOnUpgrade                  bool `json:"supportsOnUpgrade"`
	SupportsOnRename                   bool `json:"supportsOnRename"`
	SupportsOnAuthorAdded              bool `json:"supportsOnAuthorAdded"`
	SupportsOnBookAdded                bool `json:"supportsOnBookAdded"`
	SupportsOnAuthorDelete             bool `json:"supportsOnAuthorDelete"`
	SupportsOnBookDelete               bool `json:"supportsOnBookDelete"`
	SupportsOnBookFileDelete           bool `json:"supportsOnBookFileDelete"`
	SupportsOnBookFileDeleteForUpgrade bool `json:"supportsOnBookFileDeleteForUpgrade"`
	SupportsOnHealthIssue              bool `json:"supportsOnHealthIssue"`
	SupportsOnDownloadFailure          bool `json:"supportsOnDownloadFailure"`
	SupportsOnImportFailure            bool `json:"supportsOnImportFailure"`
	SupportsOnBookRetag                bool `json:"supportsOnBookRetag"`
	SupportsOnApplicationUpdate        bool `json:"supportsOnApplicationUpdate"`
}

func newNotificationResource() resource.Resource { return &notificationResource{} }
func (r *notificationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification"
}
func (r *notificationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	a := integrationBaseAttributes()
	for _, name := range []string{"on_grab", "on_release_import", "on_upgrade", "on_rename", "on_author_added", "on_book_added", "on_author_delete", "on_book_delete", "on_book_file_delete", "on_book_file_delete_for_upgrade", "on_health_issue", "include_health_warnings", "on_download_failure", "on_import_failure", "on_book_retag", "on_application_update"} {
		a[name] = schema.BoolAttribute{Required: true}
	}
	a["supported_events"] = schema.SetAttribute{Computed: true, ElementType: types.StringType}
	resp.Schema = schema.Schema{MarkdownDescription: "Manage a Chaptarr notification provider such as ntfy. Dynamic settings are schema-validated; tokens and passwords are apply-only. Test actions are never invoked.", Attributes: a}
}
func (r *notificationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configuredResourceClient(req.ProviderData, &resp.Diagnostics)
}
func (r *notificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var p notificationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	loadIntegrationSecrets(ctx, req.Config, &p.SecretFields, &resp.Diagnostics)
	payload := notificationPayload(ctx, p, 0, &resp.Diagnostics)
	if !validateIntegrationActivation(p.Enable, p.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/notification/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	id := createProfile(ctx, r.client, "/api/v1/notification", payload, "notification", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	p.ID = types.StringValue(strconv.FormatInt(id, 10))
	r.refresh(ctx, &p, &resp.State, &resp.Diagnostics)
}
func (r *notificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var s notificationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &s, &resp.State, &resp.Diagnostics)
	}
}
func (r *notificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var p notificationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &p)...)
	loadIntegrationSecrets(ctx, req.Config, &p.SecretFields, &resp.Diagnostics)
	id, ok := positiveModelID(p.ID)
	if resp.Diagnostics.HasError() || !ok {
		return
	}
	payload := notificationPayload(ctx, p, id, &resp.Diagnostics)
	if !validateIntegrationActivation(p.Enable, p.TestOnApply, &resp.Diagnostics) || resp.Diagnostics.HasError() || !validateIntegrationFields(ctx, r.client, "/api/v1/notification/schema", payload.Implementation, payload.ConfigContract, payload.Fields, &resp.Diagnostics) {
		return
	}
	updateProfile(ctx, r.client, "/api/v1/notification/"+strconv.FormatInt(id, 10), payload, "notification", &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		r.refresh(ctx, &p, &resp.State, &resp.Diagnostics)
	}
}
func (r *notificationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var s notificationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
	if !resp.Diagnostics.HasError() {
		deleteProfile(ctx, r.client, "/api/v1/notification/", s.ID, "notification", &resp.Diagnostics)
	}
}
func (r *notificationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importNumericProfile(ctx, req.ID, "notification", &resp.State, &resp.Diagnostics)
}
func notificationPayload(ctx context.Context, p notificationModel, id int64, d *diag.Diagnostics) notificationAPI {
	supported := map[string]bool{}
	for _, name := range setStringValues(ctx, p.SupportedEvents, d) {
		supported[name] = true
	}
	return notificationAPI{integrationBaseAPI: integrationBasePayload(ctx, id, p.Name, p.Implementation, p.ConfigContract, p.Enable, p.Tags, p.FieldValuesJSON, p.SecretFields, d), OnGrab: valueBool(p.OnGrab), OnReleaseImport: valueBool(p.OnReleaseImport), OnUpgrade: valueBool(p.OnUpgrade), OnRename: valueBool(p.OnRename), OnAuthorAdded: valueBool(p.OnAuthorAdded), OnBookAdded: valueBool(p.OnBookAdded), OnAuthorDelete: valueBool(p.OnAuthorDelete), OnBookDelete: valueBool(p.OnBookDelete), OnBookFileDelete: valueBool(p.OnBookFileDelete), OnBookFileDeleteForUpgrade: valueBool(p.OnBookFileDeleteForUpgrade), OnHealthIssue: valueBool(p.OnHealthIssue), IncludeHealthWarnings: valueBool(p.IncludeHealthWarnings), OnDownloadFailure: valueBool(p.OnDownloadFailure), OnImportFailure: valueBool(p.OnImportFailure), OnBookRetag: valueBool(p.OnBookRetag), OnApplicationUpdate: valueBool(p.OnApplicationUpdate), SupportsOnGrab: supported["grab"], SupportsOnReleaseImport: supported["release_import"], SupportsOnUpgrade: supported["upgrade"], SupportsOnRename: supported["rename"], SupportsOnAuthorAdded: supported["author_added"], SupportsOnBookAdded: supported["book_added"], SupportsOnAuthorDelete: supported["author_delete"], SupportsOnBookDelete: supported["book_delete"], SupportsOnBookFileDelete: supported["book_file_delete"], SupportsOnBookFileDeleteForUpgrade: supported["book_file_delete_for_upgrade"], SupportsOnHealthIssue: supported["health_issue"], SupportsOnDownloadFailure: supported["download_failure"], SupportsOnImportFailure: supported["import_failure"], SupportsOnBookRetag: supported["book_retag"], SupportsOnApplicationUpdate: supported["application_update"]}
}
func (r *notificationResource) refresh(ctx context.Context, s *notificationModel, target *tfsdk.State, d *diag.Diagnostics) {
	id, ok := positiveModelID(s.ID)
	if !ok {
		return
	}
	body, found := readProfile(ctx, r.client, "/api/v1/notification/"+strconv.FormatInt(id, 10), "notification", target, d)
	if !found || d.HasError() {
		return
	}
	var c notificationAPI
	if json.Unmarshal(body, &c) != nil || c.ID < 1 || strings.TrimSpace(c.Name) == "" {
		d.AddError("Invalid Chaptarr response", "Chaptarr returned an invalid notification document.")
		return
	}
	setIntegrationBaseState(ctx, c.integrationBaseAPI, &s.ID, &s.Name, &s.ImplementationName, &s.Implementation, &s.ConfigContract, &s.Enable, &s.Tags, &s.FieldValuesJSON, &s.FieldValuesSHA256, &s.SecretFields, &s.ProtectedFieldNames, d)
	normalizeIntegrationTestAuthorization(&s.TestOnApply)
	s.OnGrab = types.BoolValue(c.OnGrab)
	s.OnReleaseImport = types.BoolValue(c.OnReleaseImport)
	s.OnUpgrade = types.BoolValue(c.OnUpgrade)
	s.OnRename = types.BoolValue(c.OnRename)
	s.OnAuthorAdded = types.BoolValue(c.OnAuthorAdded)
	s.OnBookAdded = types.BoolValue(c.OnBookAdded)
	s.OnAuthorDelete = types.BoolValue(c.OnAuthorDelete)
	s.OnBookDelete = types.BoolValue(c.OnBookDelete)
	s.OnBookFileDelete = types.BoolValue(c.OnBookFileDelete)
	s.OnBookFileDeleteForUpgrade = types.BoolValue(c.OnBookFileDeleteForUpgrade)
	s.OnHealthIssue = types.BoolValue(c.OnHealthIssue)
	s.IncludeHealthWarnings = types.BoolValue(c.IncludeHealthWarnings)
	s.OnDownloadFailure = types.BoolValue(c.OnDownloadFailure)
	s.OnImportFailure = types.BoolValue(c.OnImportFailure)
	s.OnBookRetag = types.BoolValue(c.OnBookRetag)
	s.OnApplicationUpdate = types.BoolValue(c.OnApplicationUpdate)
	events := []string{}
	support := []struct {
		name    string
		enabled bool
	}{{"grab", c.SupportsOnGrab}, {"release_import", c.SupportsOnReleaseImport}, {"upgrade", c.SupportsOnUpgrade}, {"rename", c.SupportsOnRename}, {"author_added", c.SupportsOnAuthorAdded}, {"book_added", c.SupportsOnBookAdded}, {"author_delete", c.SupportsOnAuthorDelete}, {"book_delete", c.SupportsOnBookDelete}, {"book_file_delete", c.SupportsOnBookFileDelete}, {"book_file_delete_for_upgrade", c.SupportsOnBookFileDeleteForUpgrade}, {"health_issue", c.SupportsOnHealthIssue}, {"download_failure", c.SupportsOnDownloadFailure}, {"import_failure", c.SupportsOnImportFailure}, {"book_retag", c.SupportsOnBookRetag}, {"application_update", c.SupportsOnApplicationUpdate}}
	for _, event := range support {
		if event.enabled {
			events = append(events, event.name)
		}
	}
	s.SupportedEvents = setStringState(ctx, events, d)
	d.Append(target.Set(ctx, s)...)
}
