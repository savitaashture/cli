package service

import (
	"strings"

	"github.com/cloudfoundry/cli/cf"
	"github.com/cloudfoundry/cli/cf/api"
	"github.com/cloudfoundry/cli/cf/api/applications"
	"github.com/cloudfoundry/cli/cf/command_metadata"
	"github.com/cloudfoundry/cli/cf/configuration/core_config"
	"github.com/cloudfoundry/cli/cf/errors"
	. "github.com/cloudfoundry/cli/cf/i18n"
	"github.com/cloudfoundry/cli/cf/models"
	"github.com/cloudfoundry/cli/cf/requirements"
	"github.com/cloudfoundry/cli/cf/terminal"
	"github.com/codegangsta/cli"
)

type BindService struct {
	ui                 terminal.UI
	config             core_config.Reader
	serviceBindingRepo api.ServiceBindingRepository
	appReq             requirements.ApplicationRequirement
	appRepo            applications.ApplicationRepository
	serviceInstanceReq requirements.ServiceInstanceRequirement
	appStagingWatcher  ApplicationStagingWatcher
}

type ServiceBinder interface {
	BindApplication(app models.Application, serviceInstance models.ServiceInstance) (apiErr error)
}

type ApplicationStagingWatcher interface {
	ApplicationWatchStaging(app models.Application, orgName string, spaceName string, startCommand func(app models.Application) (models.Application, error)) (updatedApp models.Application, err error)
}

func NewBindService(ui terminal.UI, config core_config.Reader, serviceBindingRepo api.ServiceBindingRepository, appRepo applications.ApplicationRepository, stagingWatcher ApplicationStagingWatcher) (cmd *BindService) {
	cmd = new(BindService)
	cmd.ui = ui
	cmd.config = config
	cmd.serviceBindingRepo = serviceBindingRepo
	cmd.appRepo = appRepo
	cmd.appStagingWatcher = stagingWatcher
	return
}

func (cmd *BindService) Metadata() command_metadata.CommandMetadata {
	flagUsage := T("Restage app")
	tipUsage := T("TIP: Changes will not apply to existing running applications until they are restaged. Use `bind-service --force-restage` to force restage app.")
	return command_metadata.CommandMetadata{
		Name:        "bind-service",
		ShortName:   "bs",
		Description: T("Bind a service instance to an app"),
		Usage:       T("CF_NAME bind-service APP_NAME SERVICE_INSTANCE"),
		Flags: []cli.Flag{
			cli.BoolFlag{Name: "force-restage", Usage: strings.Join([]string{flagUsage, tipUsage}, "\n\n")},
		},
	}
}

func (cmd *BindService) GetRequirements(requirementsFactory requirements.Factory, c *cli.Context) (reqs []requirements.Requirement, err error) {

	if len(c.Args()) != 2 {
		cmd.ui.FailWithUsage(c)
	}

	serviceName := c.Args()[1]

	if cmd.appReq == nil {
		cmd.appReq = requirementsFactory.NewApplicationRequirement(c.Args()[0])
	} else {
		cmd.appReq.SetApplicationName(c.Args()[0])
	}

	cmd.serviceInstanceReq = requirementsFactory.NewServiceInstanceRequirement(serviceName)

	reqs = []requirements.Requirement{requirementsFactory.NewLoginRequirement(), cmd.appReq, cmd.serviceInstanceReq}
	return
}

func (cmd *BindService) Run(c *cli.Context) {
	app := cmd.appReq.GetApplication()
	serviceInstance := cmd.serviceInstanceReq.GetServiceInstance()
	restageFlag := c.Bool("force-restage")

	cmd.ui.Say(T("Binding service {{.ServiceInstanceName}} to app {{.AppName}} in org {{.OrgName}} / space {{.SpaceName}} as {{.CurrentUser}}...",
		map[string]interface{}{
			"ServiceInstanceName": terminal.EntityNameColor(serviceInstance.Name),
			"AppName":             terminal.EntityNameColor(app.Name),
			"OrgName":             terminal.EntityNameColor(cmd.config.OrganizationFields().Name),
			"SpaceName":           terminal.EntityNameColor(cmd.config.SpaceFields().Name),
			"CurrentUser":         terminal.EntityNameColor(cmd.config.Username()),
		}))

	err := cmd.BindApplication(app, serviceInstance)
	if err != nil {
		if httperr, ok := err.(errors.HttpError); ok && httperr.ErrorCode() == errors.APP_ALREADY_BOUND {
			cmd.ui.Ok()
			cmd.ui.Warn(T("App {{.AppName}} is already bound to {{.ServiceName}}.",
				map[string]interface{}{
					"AppName":     app.Name,
					"ServiceName": serviceInstance.Name,
				}))
			return
		} else {
			cmd.ui.Failed(err.Error())
		}
	}

	cmd.ui.Ok()

	if true == restageFlag {
		cmd.ui.Say("")
		cmd.ui.Say(T("Restaging app {{.AppName}} in org {{.OrgName}} / space {{.SpaceName}} as {{.CurrentUser}}...",
			map[string]interface{}{
				"AppName":     terminal.EntityNameColor(app.Name),
				"OrgName":     terminal.EntityNameColor(cmd.config.OrganizationFields().Name),
				"SpaceName":   terminal.EntityNameColor(cmd.config.SpaceFields().Name),
				"CurrentUser": terminal.EntityNameColor(cmd.config.Username()),
			}))

		cmd.appStagingWatcher.ApplicationWatchStaging(app, cmd.config.OrganizationFields().Name, cmd.config.SpaceFields().Name, func(app models.Application) (models.Application, error) {
			return app, cmd.appRepo.CreateRestageRequest(app.Guid)
		})
	} else {
		cmd.ui.Say(T("TIP: Use '{{.CFCommand}}' to ensure your env variable changes take effect",
			map[string]interface{}{"CFCommand": terminal.CommandColor(cf.Name() + " restage")}))
	}
}

func (cmd *BindService) BindApplication(app models.Application, serviceInstance models.ServiceInstance) (apiErr error) {
	apiErr = cmd.serviceBindingRepo.Create(serviceInstance.Guid, app.Guid)
	return
}
