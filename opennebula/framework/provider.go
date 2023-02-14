package opennebula

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/OpenNebula/one/src/oca/go/src/goca"
	ver "github.com/hashicorp/go-version"

	"github.com/OpenNebula/terraform-provider-opennebula/opennebula/framework/config"
	providerCfg "github.com/OpenNebula/terraform-provider-opennebula/opennebula/framework/config"
	"github.com/OpenNebula/terraform-provider-opennebula/opennebula/framework/resources"
	"github.com/OpenNebula/terraform-provider-opennebula/opennebula/framework/utils"
)

type OpenNebulaProvider struct {
	config.Provider
}

func New() provider.Provider {
	return &OpenNebulaProvider{}
}

// Metadata returns the provider type name.
func (p *OpenNebulaProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "opennebula"
}

type opennebulaProviderModel struct {
	Endpoint     types.String `tfsdk:"endpoint"`
	FlowEndpoint types.String `tfsdk:"flow_endpoint"`
	Username     types.String `tfsdk:"username"`
	Password     types.String `tfsdk:"password"`
	Insecure     types.Bool   `tfsdk:"insecure"`
	DefaultTags  types.Set    `tfsdk:"default_tags"`
}

func (p *OpenNebulaProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "The URL to your public or private OpenNebula",
				//MarkdownDescription: "",
			},
			"flow_endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "The URL to your public or private OpenNebula Flow server",
				//MarkdownDescription: "",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Description: "The ID of the user to identify as",
				//MarkdownDescription: "",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Description: "The password for the user",
				//MarkdownDescription: "",
			},
			"insecure": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable TLS validation",
				//MarkdownDescription: "",
			},
		},
		Blocks: map[string]schema.Block{
			"default_tags": schema.SetNestedBlock{
				Description: "Add default tags to the resources",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"tags": schema.MapAttribute{
							Optional:    true,
							Description: "Default tags to apply",
							ElementType: types.StringType,
						},
					},
				},
			},
		},
	}
}

func (p *OpenNebulaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config opennebulaProviderModel

	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Unknown OpenNebula XML-RPC API endpoint",
			"The provider cannot create the OpenNebula XML-RPC client as there is an unknown configuration value for the OpenNebula API endpoint. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the OPENNEBULA_ENDPOINT environment variable.",
		)
	}

	if config.Username.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("username"),
			"Unknown OpenNebula XML-RPC API username",
			"The provider cannot create the OpenNebula XML-RPC client as there is an unknown configuration value for the OpenNebula API username. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the OPENNEBULA_USERNAME environment variable.",
		)
	}

	if config.Password.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("password"),
			"Unknown OpenNebula XML-RPC API password",
			"The provider cannot create the OpenNebula XML-RPC client as there is an unknown configuration value for the OpenNebula API password. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the OPENNEBULA_PASSWORD environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.

	endpoint := os.Getenv("OPENNEBULA_ENDPOINT")
	flowEndpoint := os.Getenv("OPENNEBULA_FLOW_ENDPOINT")
	username := os.Getenv("OPENNEBULA_USERNAME")
	password := os.Getenv("OPENNEBULA_PASSWORD")

	insecureStr := os.Getenv("OPENNEBULA_INSECURE")
	insecure := false

	var err error
	if len(insecureStr) > 0 {
		insecure, err = strconv.ParseBool(insecureStr)
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("insecure"),
				"Failed to parse boolean value from the OPENNEBULA_INSECURE environment variable",
				"The provider cannot create the OpenNebula XML-RPC client as there is an unknown configuration value for the OPENNEBULA_INSECURE environment variable.",
			)
		}
	}

	if !config.Endpoint.IsNull() {
		endpoint = config.Endpoint.ValueString()
	}

	if !config.Endpoint.IsNull() {
		flowEndpoint = config.Endpoint.ValueString()
	}

	if !config.Username.IsNull() {
		username = config.Username.ValueString()
	}

	if !config.Password.IsNull() {
		password = config.Password.ValueString()
	}

	if !config.Insecure.IsNull() {
		insecure = config.Insecure.ValueBool()
	}

	// If any of the expected configurations are missing, return
	// errors with provider-specific guidance.

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing OpenNebula XML-RPC endpoint",
			"The provider cannot create the OpenNebula XML-RPC client as there is a missing or empty value for the OpenNebula API endpoint. "+
				"Set the endpoint value in the configuration or use the OPENNEBULA_ENDPOINT environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if username == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("username"),
			"Missing OpenNebula account username",
			"The provider cannot create the OpenNebula XML-RPC client as there is a missing or empty value for the OpenNebula username. "+
				"Set the endpoint value in the configuration or use the OPENNEBULA_USERNAME environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if password == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing OpenNebula account password",
			"The provider cannot create the OpenNebula XML-RPC client as there is a missing or empty value for the OpenNebula password. "+
				"Set the endpoint value in the configuration or use the OPENNEBULA_PASSWORD environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	// Create a new OpenNebula client using the configuration values
	client := goca.NewClient(goca.NewConfig(username,
		password,
		endpoint),
		&http.Client{Transport: tr})

	versionStr, err := goca.NewController(client).SystemVersion()
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to get OpenNebula release number",
			err.Error(),
		)
		return
	}
	version, err := ver.NewVersion(versionStr)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to parse OpenNebula version",
			err.Error(),
		)
		return
	}

	log.Printf("[INFO] OpenNebula version: %s", versionStr)

	cfg := &OpenNebulaProvider{
		providerCfg.Provider{
			OneVersion: version,
			Mutex:      *utils.NewMutexKV(),
		},
	}

	if len(flowEndpoint) > 0 {
		flowClient := goca.NewDefaultFlowClient(
			goca.NewFlowConfig(username,
				password,
				flowEndpoint))

		cfg.Controller = goca.NewGenericController(client, flowClient)
	} else {
		cfg.Controller = goca.NewController(client)
	}

	var tags DefaultTags
	for _, t := range config.DefaultTags.Elements() {
		element, err := t.ToTerraformValue(ctx)
		if err != nil {
			log.Print("[DEBUG] ToTerraformValue err: ", err)
			continue
		}
		err = element.As(&tags)
		if err != nil {
			log.Print("[DEBUG] As err: ", err)
			continue
		}
	}

	if len(tags.Elements) > 0 {
		cfg.DefaultTags = tags.Elements
	}
	log.Printf("[DEBUG] default_tags: %+v", tags.Elements)

	// Make the OpenNebula controller available during DataSource and Resource
	// type Configure methods.
	resp.DataSourceData = &cfg.Provider
	resp.ResourceData = &cfg.Provider

}

func (p *OpenNebulaProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource {
			return resources.NewCluster()
		},
	}
}

func (p *OpenNebulaProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		func() datasource.DataSource {
			return NewExampleDataSource()
		},
	}
}