// Copyright (C) 2019-2023 Nicola Murino
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package httpd

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/sftpgo/sdk"
	sdkkms "github.com/sftpgo/sdk/kms"

	"github.com/drakkan/sftpgo/v2/internal/acme"
	"github.com/drakkan/sftpgo/v2/internal/common"
	"github.com/drakkan/sftpgo/v2/internal/dataprovider"
	"github.com/drakkan/sftpgo/v2/internal/kms"
	"github.com/drakkan/sftpgo/v2/internal/logger"
	"github.com/drakkan/sftpgo/v2/internal/mfa"
	"github.com/drakkan/sftpgo/v2/internal/plugin"
	"github.com/drakkan/sftpgo/v2/internal/smtp"
	"github.com/drakkan/sftpgo/v2/internal/util"
	"github.com/drakkan/sftpgo/v2/internal/version"
	"github.com/drakkan/sftpgo/v2/internal/vfs"
)

type userPageMode int

const (
	userPageModeAdd userPageMode = iota + 1
	userPageModeUpdate
	userPageModeTemplate
)

type folderPageMode int

const (
	folderPageModeAdd folderPageMode = iota + 1
	folderPageModeUpdate
	folderPageModeTemplate
)

type genericPageMode int

const (
	genericPageModeAdd genericPageMode = iota + 1
	genericPageModeUpdate
)

const (
	templateAdminDir         = "webadmin"
	templateBase             = "base.html"
	templateBaseLogin        = "baselogin.html"
	templateFsConfig         = "fsconfig.html"
	templateSharedComponents = "sharedcomponents.html"
	templateUsers            = "users.html"
	templateUser             = "user.html"
	templateAdmins           = "admins.html"
	templateAdmin            = "admin.html"
	templateConnections      = "connections.html"
	templateGroups           = "groups.html"
	templateGroup            = "group.html"
	templateFolders          = "folders.html"
	templateFolder           = "folder.html"
	templateEventRules       = "eventrules.html"
	templateEventRule        = "eventrule.html"
	templateEventActions     = "eventactions.html"
	templateEventAction      = "eventaction.html"
	templateRoles            = "roles.html"
	templateRole             = "role.html"
	templateEvents           = "events.html"
	templateMessage          = "message.html"
	templateStatus           = "status.html"
	templateLogin            = "login.html"
	templateDefender         = "defender.html"
	templateIPLists          = "iplists.html"
	templateIPList           = "iplist.html"
	templateConfigs          = "configs.html"
	templateProfile          = "profile.html"
	templateChangePwd        = "changepassword.html"
	templateMaintenance      = "maintenance.html"
	templateMFA              = "mfa.html"
	templateSetup            = "adminsetup.html"
	pageUsersTitle           = "Users"
	pageAdminsTitle          = "Admins"
	pageConnectionsTitle     = "Connections"
	pageStatusTitle          = "Status"
	pageFoldersTitle         = "Folders"
	pageGroupsTitle          = "Groups"
	pageEventRulesTitle      = "Event rules"
	pageEventActionsTitle    = "Event actions"
	pageRolesTitle           = "Roles"
	pageProfileTitle         = "My profile"
	pageChangePwdTitle       = "Change password"
	pageMaintenanceTitle     = "Maintenance"
	pageDefenderTitle        = "Auto Blocklist"
	pageIPListsTitle         = "IP Lists"
	pageEventsTitle          = "Logs"
	pageConfigsTitle         = "Configurations"
	pageForgotPwdTitle       = "SFTPGo Admin - Forgot password"
	pageResetPwdTitle        = "SFTPGo Admin - Reset password"
	pageSetupTitle           = "Create first admin user"
	defaultQueryLimit        = 500
	inversePatternType       = "inverse"
)

var (
	adminTemplates = make(map[string]*template.Template)
)

type basePage struct {
	Title               string
	CurrentURL          string
	UsersURL            string
	UserURL             string
	UserTemplateURL     string
	AdminsURL           string
	AdminURL            string
	QuotaScanURL        string
	ConnectionsURL      string
	GroupsURL           string
	GroupURL            string
	FoldersURL          string
	FolderURL           string
	FolderTemplateURL   string
	DefenderURL         string
	IPListsURL          string
	IPListURL           string
	EventsURL           string
	ConfigsURL          string
	LogoutURL           string
	ProfileURL          string
	ChangePwdURL        string
	MFAURL              string
	EventRulesURL       string
	EventRuleURL        string
	EventActionsURL     string
	EventActionURL      string
	RolesURL            string
	RoleURL             string
	FolderQuotaScanURL  string
	StatusURL           string
	MaintenanceURL      string
	StaticURL           string
	UsersTitle          string
	AdminsTitle         string
	ConnectionsTitle    string
	FoldersTitle        string
	GroupsTitle         string
	EventRulesTitle     string
	EventActionsTitle   string
	RolesTitle          string
	StatusTitle         string
	MaintenanceTitle    string
	DefenderTitle       string
	IPListsTitle        string
	EventsTitle         string
	ConfigsTitle        string
	Version             string
	CSRFToken           string
	IsEventManagerPage  bool
	IsIPManagerPage     bool
	IsServerManagerPage bool
	HasDefender         bool
	HasSearcher         bool
	HasExternalLogin    bool
	LoggedAdmin         *dataprovider.Admin
	Branding            UIBranding
}

type usersPage struct {
	basePage
	Users []dataprovider.User
}

type adminsPage struct {
	basePage
	Admins []dataprovider.Admin
}

type foldersPage struct {
	basePage
	Folders []vfs.BaseVirtualFolder
}

type groupsPage struct {
	basePage
	Groups []dataprovider.Group
}

type rolesPage struct {
	basePage
	Roles []dataprovider.Role
}

type eventRulesPage struct {
	basePage
	Rules []dataprovider.EventRule
}

type eventActionsPage struct {
	basePage
	Actions []dataprovider.BaseEventAction
}

type connectionsPage struct {
	basePage
	Connections []common.ConnectionStatus
}

type statusPage struct {
	basePage
	Status *ServicesStatus
}

type fsWrapper struct {
	vfs.Filesystem
	IsUserPage      bool
	IsGroupPage     bool
	IsHidden        bool
	HasUsersBaseDir bool
	DirPath         string
}

type userPage struct {
	basePage
	User               *dataprovider.User
	RootPerms          []string
	Error              string
	ValidPerms         []string
	ValidLoginMethods  []string
	ValidProtocols     []string
	TwoFactorProtocols []string
	WebClientOptions   []string
	RootDirPerms       []string
	Mode               userPageMode
	VirtualFolders     []vfs.BaseVirtualFolder
	Groups             []dataprovider.Group
	Roles              []dataprovider.Role
	CanImpersonate     bool
	FsWrapper          fsWrapper
}

type adminPage struct {
	basePage
	Admin  *dataprovider.Admin
	Groups []dataprovider.Group
	Roles  []dataprovider.Role
	Error  string
	IsAdd  bool
}

type profilePage struct {
	basePage
	Error           string
	AllowAPIKeyAuth bool
	Email           string
	Description     string
}

type changePasswordPage struct {
	basePage
	Error string
}

type mfaPage struct {
	basePage
	TOTPConfigs     []string
	TOTPConfig      dataprovider.AdminTOTPConfig
	GenerateTOTPURL string
	ValidateTOTPURL string
	SaveTOTPURL     string
	RecCodesURL     string
}

type maintenancePage struct {
	basePage
	BackupPath  string
	RestorePath string
	Error       string
}

type defenderHostsPage struct {
	basePage
	DefenderHostsURL string
}

type ipListsPage struct {
	basePage
	IPListsSearchURL      string
	RateLimitersStatus    bool
	RateLimitersProtocols string
	IsAllowListEnabled    bool
}

type ipListPage struct {
	basePage
	Entry *dataprovider.IPListEntry
	Error string
	Mode  genericPageMode
}

type setupPage struct {
	basePage
	Username             string
	HasInstallationCode  bool
	InstallationCodeHint string
	HideSupportLink      bool
	Error                string
}

type folderPage struct {
	basePage
	Folder    vfs.BaseVirtualFolder
	Error     string
	Mode      folderPageMode
	FsWrapper fsWrapper
}

type groupPage struct {
	basePage
	Group              *dataprovider.Group
	Error              string
	Mode               genericPageMode
	ValidPerms         []string
	ValidLoginMethods  []string
	ValidProtocols     []string
	TwoFactorProtocols []string
	WebClientOptions   []string
	VirtualFolders     []vfs.BaseVirtualFolder
	FsWrapper          fsWrapper
}

type rolePage struct {
	basePage
	Role  *dataprovider.Role
	Error string
	Mode  genericPageMode
}

type eventActionPage struct {
	basePage
	Action         dataprovider.BaseEventAction
	ActionTypes    []dataprovider.EnumMapping
	FsActions      []dataprovider.EnumMapping
	HTTPMethods    []string
	RedactedSecret string
	Error          string
	Mode           genericPageMode
}

type eventRulePage struct {
	basePage
	Rule            dataprovider.EventRule
	TriggerTypes    []dataprovider.EnumMapping
	Actions         []dataprovider.BaseEventAction
	FsEvents        []string
	Protocols       []string
	ProviderEvents  []string
	ProviderObjects []string
	Error           string
	Mode            genericPageMode
	IsShared        bool
}

type eventsPage struct {
	basePage
	FsEventsSearchURL       string
	ProviderEventsSearchURL string
	LogEventsSearchURL      string
}

type configsPage struct {
	basePage
	Configs           dataprovider.Configs
	ConfigSection     int
	RedactedSecret    string
	OAuth2TokenURL    string
	OAuth2RedirectURL string
	Error             string
}

type messagePage struct {
	basePage
	Error   string
	Success string
}

type userTemplateFields struct {
	Username   string
	Password   string
	PublicKeys []string
}

func loadAdminTemplates(templatesPath string) {
	usersPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateUsers),
	}
	userPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateSharedComponents),
		filepath.Join(templatesPath, templateAdminDir, templateFsConfig),
		filepath.Join(templatesPath, templateAdminDir, templateUser),
	}
	adminsPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateAdmins),
	}
	adminPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateAdmin),
	}
	profilePaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateProfile),
	}
	changePwdPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateChangePwd),
	}
	connectionsPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateConnections),
	}
	messagePaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateMessage),
	}
	foldersPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateFolders),
	}
	folderPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateFsConfig),
		filepath.Join(templatesPath, templateAdminDir, templateFolder),
	}
	groupsPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateGroups),
	}
	groupPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateFsConfig),
		filepath.Join(templatesPath, templateAdminDir, templateSharedComponents),
		filepath.Join(templatesPath, templateAdminDir, templateGroup),
	}
	eventRulesPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateEventRules),
	}
	eventRulePaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateEventRule),
	}
	eventActionsPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateEventActions),
	}
	eventActionPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateEventAction),
	}
	statusPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateStatus),
	}
	loginPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBaseLogin),
		filepath.Join(templatesPath, templateAdminDir, templateLogin),
	}
	maintenancePaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateMaintenance),
	}
	defenderPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateDefender),
	}
	ipListsPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateIPLists),
	}
	ipListPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateIPList),
	}
	mfaPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateMFA),
	}
	twoFactorPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBaseLogin),
		filepath.Join(templatesPath, templateAdminDir, templateTwoFactor),
	}
	twoFactorRecoveryPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBaseLogin),
		filepath.Join(templatesPath, templateAdminDir, templateTwoFactorRecovery),
	}
	setupPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBaseLogin),
		filepath.Join(templatesPath, templateAdminDir, templateSetup),
	}
	forgotPwdPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateCommonDir, templateForgotPassword),
	}
	resetPwdPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateCommonDir, templateResetPassword),
	}
	rolesPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateRoles),
	}
	rolePaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateRole),
	}
	eventsPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateEvents),
	}
	configsPaths := []string{
		filepath.Join(templatesPath, templateCommonDir, templateCommonCSS),
		filepath.Join(templatesPath, templateAdminDir, templateBase),
		filepath.Join(templatesPath, templateAdminDir, templateConfigs),
	}

	fsBaseTpl := template.New("fsBaseTemplate").Funcs(template.FuncMap{
		"ListFSProviders": func() []sdk.FilesystemProvider {
			return []sdk.FilesystemProvider{sdk.LocalFilesystemProvider, sdk.CryptedFilesystemProvider,
				sdk.S3FilesystemProvider, sdk.GCSFilesystemProvider, sdk.AzureBlobFilesystemProvider,
				sdk.SFTPFilesystemProvider, sdk.HTTPFilesystemProvider,
			}
		},
		"HumanizeBytes": util.ByteCountSI,
	})
	usersTmpl := util.LoadTemplate(nil, usersPaths...)
	userTmpl := util.LoadTemplate(fsBaseTpl, userPaths...)
	adminsTmpl := util.LoadTemplate(nil, adminsPaths...)
	adminTmpl := util.LoadTemplate(nil, adminPaths...)
	connectionsTmpl := util.LoadTemplate(nil, connectionsPaths...)
	messageTmpl := util.LoadTemplate(nil, messagePaths...)
	groupsTmpl := util.LoadTemplate(nil, groupsPaths...)
	groupTmpl := util.LoadTemplate(fsBaseTpl, groupPaths...)
	foldersTmpl := util.LoadTemplate(nil, foldersPaths...)
	folderTmpl := util.LoadTemplate(fsBaseTpl, folderPaths...)
	eventRulesTmpl := util.LoadTemplate(nil, eventRulesPaths...)
	eventRuleTmpl := util.LoadTemplate(fsBaseTpl, eventRulePaths...)
	eventActionsTmpl := util.LoadTemplate(nil, eventActionsPaths...)
	eventActionTmpl := util.LoadTemplate(nil, eventActionPaths...)
	statusTmpl := util.LoadTemplate(nil, statusPaths...)
	loginTmpl := util.LoadTemplate(nil, loginPaths...)
	profileTmpl := util.LoadTemplate(nil, profilePaths...)
	changePwdTmpl := util.LoadTemplate(nil, changePwdPaths...)
	maintenanceTmpl := util.LoadTemplate(nil, maintenancePaths...)
	defenderTmpl := util.LoadTemplate(nil, defenderPaths...)
	ipListsTmpl := util.LoadTemplate(nil, ipListsPaths...)
	ipListTmpl := util.LoadTemplate(nil, ipListPaths...)
	mfaTmpl := util.LoadTemplate(nil, mfaPaths...)
	twoFactorTmpl := util.LoadTemplate(nil, twoFactorPaths...)
	twoFactorRecoveryTmpl := util.LoadTemplate(nil, twoFactorRecoveryPaths...)
	setupTmpl := util.LoadTemplate(nil, setupPaths...)
	forgotPwdTmpl := util.LoadTemplate(nil, forgotPwdPaths...)
	resetPwdTmpl := util.LoadTemplate(nil, resetPwdPaths...)
	rolesTmpl := util.LoadTemplate(nil, rolesPaths...)
	roleTmpl := util.LoadTemplate(nil, rolePaths...)
	eventsTmpl := util.LoadTemplate(nil, eventsPaths...)
	configsTmpl := util.LoadTemplate(nil, configsPaths...)

	adminTemplates[templateUsers] = usersTmpl
	adminTemplates[templateUser] = userTmpl
	adminTemplates[templateAdmins] = adminsTmpl
	adminTemplates[templateAdmin] = adminTmpl
	adminTemplates[templateConnections] = connectionsTmpl
	adminTemplates[templateMessage] = messageTmpl
	adminTemplates[templateGroups] = groupsTmpl
	adminTemplates[templateGroup] = groupTmpl
	adminTemplates[templateFolders] = foldersTmpl
	adminTemplates[templateFolder] = folderTmpl
	adminTemplates[templateEventRules] = eventRulesTmpl
	adminTemplates[templateEventRule] = eventRuleTmpl
	adminTemplates[templateEventActions] = eventActionsTmpl
	adminTemplates[templateEventAction] = eventActionTmpl
	adminTemplates[templateStatus] = statusTmpl
	adminTemplates[templateLogin] = loginTmpl
	adminTemplates[templateProfile] = profileTmpl
	adminTemplates[templateChangePwd] = changePwdTmpl
	adminTemplates[templateMaintenance] = maintenanceTmpl
	adminTemplates[templateDefender] = defenderTmpl
	adminTemplates[templateIPLists] = ipListsTmpl
	adminTemplates[templateIPList] = ipListTmpl
	adminTemplates[templateMFA] = mfaTmpl
	adminTemplates[templateTwoFactor] = twoFactorTmpl
	adminTemplates[templateTwoFactorRecovery] = twoFactorRecoveryTmpl
	adminTemplates[templateSetup] = setupTmpl
	adminTemplates[templateForgotPassword] = forgotPwdTmpl
	adminTemplates[templateResetPassword] = resetPwdTmpl
	adminTemplates[templateRoles] = rolesTmpl
	adminTemplates[templateRole] = roleTmpl
	adminTemplates[templateEvents] = eventsTmpl
	adminTemplates[templateConfigs] = configsTmpl
}

func isEventManagerResource(currentURL string) bool {
	if currentURL == webAdminEventRulesPath {
		return true
	}
	if currentURL == webAdminEventActionsPath {
		return true
	}
	if currentURL == webAdminEventRulePath || strings.HasPrefix(currentURL, webAdminEventRulePath+"/") {
		return true
	}
	if currentURL == webAdminEventActionPath || strings.HasPrefix(currentURL, webAdminEventActionPath+"/") {
		return true
	}
	return false
}

func isIPListsResource(currentURL string) bool {
	if currentURL == webDefenderPath {
		return true
	}
	if currentURL == webIPListsPath {
		return true
	}
	if strings.HasPrefix(currentURL, webIPListPath+"/") {
		return true
	}
	return false
}

func isServerManagerResource(currentURL string) bool {
	return currentURL == webEventsPath || currentURL == webStatusPath || currentURL == webMaintenancePath ||
		currentURL == webConfigsPath
}

func (s *httpdServer) getBasePageData(title, currentURL string, r *http.Request) basePage {
	var csrfToken string
	if currentURL != "" {
		csrfToken = createCSRFToken(util.GetIPFromRemoteAddress(r.RemoteAddr))
	}
	return basePage{
		Title:               title,
		CurrentURL:          currentURL,
		UsersURL:            webUsersPath,
		UserURL:             webUserPath,
		UserTemplateURL:     webTemplateUser,
		AdminsURL:           webAdminsPath,
		AdminURL:            webAdminPath,
		GroupsURL:           webGroupsPath,
		GroupURL:            webGroupPath,
		FoldersURL:          webFoldersPath,
		FolderURL:           webFolderPath,
		FolderTemplateURL:   webTemplateFolder,
		DefenderURL:         webDefenderPath,
		IPListsURL:          webIPListsPath,
		IPListURL:           webIPListPath,
		EventsURL:           webEventsPath,
		ConfigsURL:          webConfigsPath,
		LogoutURL:           webLogoutPath,
		ProfileURL:          webAdminProfilePath,
		ChangePwdURL:        webChangeAdminPwdPath,
		MFAURL:              webAdminMFAPath,
		EventRulesURL:       webAdminEventRulesPath,
		EventRuleURL:        webAdminEventRulePath,
		EventActionsURL:     webAdminEventActionsPath,
		EventActionURL:      webAdminEventActionPath,
		RolesURL:            webAdminRolesPath,
		RoleURL:             webAdminRolePath,
		QuotaScanURL:        webQuotaScanPath,
		ConnectionsURL:      webConnectionsPath,
		StatusURL:           webStatusPath,
		FolderQuotaScanURL:  webScanVFolderPath,
		MaintenanceURL:      webMaintenancePath,
		StaticURL:           webStaticFilesPath,
		UsersTitle:          pageUsersTitle,
		AdminsTitle:         pageAdminsTitle,
		ConnectionsTitle:    pageConnectionsTitle,
		FoldersTitle:        pageFoldersTitle,
		GroupsTitle:         pageGroupsTitle,
		EventRulesTitle:     pageEventRulesTitle,
		EventActionsTitle:   pageEventActionsTitle,
		RolesTitle:          pageRolesTitle,
		StatusTitle:         pageStatusTitle,
		MaintenanceTitle:    pageMaintenanceTitle,
		DefenderTitle:       pageDefenderTitle,
		IPListsTitle:        pageIPListsTitle,
		EventsTitle:         pageEventsTitle,
		ConfigsTitle:        pageConfigsTitle,
		Version:             version.GetAsString(),
		LoggedAdmin:         getAdminFromToken(r),
		IsEventManagerPage:  isEventManagerResource(currentURL),
		IsIPManagerPage:     isIPListsResource(currentURL),
		IsServerManagerPage: isServerManagerResource(currentURL),
		HasDefender:         common.Config.DefenderConfig.Enabled,
		HasSearcher:         plugin.Handler.HasSearcher(),
		HasExternalLogin:    isLoggedInWithOIDC(r),
		CSRFToken:           csrfToken,
		Branding:            s.binding.Branding.WebAdmin,
	}
}

func renderAdminTemplate(w http.ResponseWriter, tmplName string, data any) {
	err := adminTemplates[tmplName].ExecuteTemplate(w, tmplName, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *httpdServer) renderMessagePage(w http.ResponseWriter, r *http.Request, title, body string, statusCode int,
	err error, message string,
) {
	var errorString string
	if body != "" {
		errorString = body + " "
	}
	if err != nil {
		errorString += err.Error()
	}
	data := messagePage{
		basePage: s.getBasePageData(title, "", r),
		Error:    errorString,
		Success:  message,
	}
	w.WriteHeader(statusCode)
	renderAdminTemplate(w, templateMessage, data)
}

func (s *httpdServer) renderInternalServerErrorPage(w http.ResponseWriter, r *http.Request, err error) {
	s.renderMessagePage(w, r, page500Title, page500Body, http.StatusInternalServerError, err, "")
}

func (s *httpdServer) renderBadRequestPage(w http.ResponseWriter, r *http.Request, err error) {
	s.renderMessagePage(w, r, page400Title, "", http.StatusBadRequest, err, "")
}

func (s *httpdServer) renderForbiddenPage(w http.ResponseWriter, r *http.Request, body string) {
	s.renderMessagePage(w, r, page403Title, "", http.StatusForbidden, nil, body)
}

func (s *httpdServer) renderNotFoundPage(w http.ResponseWriter, r *http.Request, err error) {
	s.renderMessagePage(w, r, page404Title, page404Body, http.StatusNotFound, err, "")
}

func (s *httpdServer) renderForgotPwdPage(w http.ResponseWriter, error, ip string) {
	data := forgotPwdPage{
		CurrentURL: webAdminForgotPwdPath,
		Error:      error,
		CSRFToken:  createCSRFToken(ip),
		StaticURL:  webStaticFilesPath,
		Title:      pageForgotPwdTitle,
		Branding:   s.binding.Branding.WebAdmin,
	}
	renderAdminTemplate(w, templateForgotPassword, data)
}

func (s *httpdServer) renderResetPwdPage(w http.ResponseWriter, error, ip string) {
	data := resetPwdPage{
		CurrentURL: webAdminResetPwdPath,
		Error:      error,
		CSRFToken:  createCSRFToken(ip),
		StaticURL:  webStaticFilesPath,
		Title:      pageResetPwdTitle,
		Branding:   s.binding.Branding.WebAdmin,
	}
	renderAdminTemplate(w, templateResetPassword, data)
}

func (s *httpdServer) renderTwoFactorPage(w http.ResponseWriter, error, ip string) {
	data := twoFactorPage{
		CurrentURL:  webAdminTwoFactorPath,
		Version:     version.Get().Version,
		Error:       error,
		CSRFToken:   createCSRFToken(ip),
		StaticURL:   webStaticFilesPath,
		RecoveryURL: webAdminTwoFactorRecoveryPath,
		Branding:    s.binding.Branding.WebAdmin,
	}
	renderAdminTemplate(w, templateTwoFactor, data)
}

func (s *httpdServer) renderTwoFactorRecoveryPage(w http.ResponseWriter, error, ip string) {
	data := twoFactorPage{
		CurrentURL: webAdminTwoFactorRecoveryPath,
		Version:    version.Get().Version,
		Error:      error,
		CSRFToken:  createCSRFToken(ip),
		StaticURL:  webStaticFilesPath,
		Branding:   s.binding.Branding.WebAdmin,
	}
	renderAdminTemplate(w, templateTwoFactorRecovery, data)
}

func (s *httpdServer) renderMFAPage(w http.ResponseWriter, r *http.Request) {
	data := mfaPage{
		basePage:        s.getBasePageData(pageMFATitle, webAdminMFAPath, r),
		TOTPConfigs:     mfa.GetAvailableTOTPConfigNames(),
		GenerateTOTPURL: webAdminTOTPGeneratePath,
		ValidateTOTPURL: webAdminTOTPValidatePath,
		SaveTOTPURL:     webAdminTOTPSavePath,
		RecCodesURL:     webAdminRecoveryCodesPath,
	}
	admin, err := dataprovider.AdminExists(data.LoggedAdmin.Username)
	if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	data.TOTPConfig = admin.Filters.TOTPConfig
	renderAdminTemplate(w, templateMFA, data)
}

func (s *httpdServer) renderProfilePage(w http.ResponseWriter, r *http.Request, error string) {
	data := profilePage{
		basePage: s.getBasePageData(pageProfileTitle, webAdminProfilePath, r),
		Error:    error,
	}
	admin, err := dataprovider.AdminExists(data.LoggedAdmin.Username)
	if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	data.AllowAPIKeyAuth = admin.Filters.AllowAPIKeyAuth
	data.Email = admin.Email
	data.Description = admin.Description

	renderAdminTemplate(w, templateProfile, data)
}

func (s *httpdServer) renderChangePasswordPage(w http.ResponseWriter, r *http.Request, error string) {
	data := changePasswordPage{
		basePage: s.getBasePageData(pageChangePwdTitle, webChangeAdminPwdPath, r),
		Error:    error,
	}

	renderAdminTemplate(w, templateChangePwd, data)
}

func (s *httpdServer) renderMaintenancePage(w http.ResponseWriter, r *http.Request, error string) {
	data := maintenancePage{
		basePage:    s.getBasePageData(pageMaintenanceTitle, webMaintenancePath, r),
		BackupPath:  webBackupPath,
		RestorePath: webRestorePath,
		Error:       error,
	}

	renderAdminTemplate(w, templateMaintenance, data)
}

func (s *httpdServer) renderConfigsPage(w http.ResponseWriter, r *http.Request, configs dataprovider.Configs,
	error string, section int,
) {
	configs.SetNilsToEmpty()
	if configs.SMTP.Port == 0 {
		configs.SMTP.Port = 587
		configs.SMTP.AuthType = 1
		configs.SMTP.Encryption = 2
	}
	if configs.ACME.HTTP01Challenge.Port == 0 {
		configs.ACME.HTTP01Challenge.Port = 80
	}
	data := configsPage{
		basePage:          s.getBasePageData(pageConfigsTitle, webConfigsPath, r),
		Configs:           configs,
		ConfigSection:     section,
		RedactedSecret:    redactedSecret,
		OAuth2TokenURL:    webOAuth2TokenPath,
		OAuth2RedirectURL: webOAuth2RedirectPath,
		Error:             error,
	}

	renderAdminTemplate(w, templateConfigs, data)
}

func (s *httpdServer) renderAdminSetupPage(w http.ResponseWriter, r *http.Request, username, error string) {
	data := setupPage{
		basePage:             s.getBasePageData(pageSetupTitle, webAdminSetupPath, r),
		Username:             username,
		HasInstallationCode:  installationCode != "",
		InstallationCodeHint: installationCodeHint,
		HideSupportLink:      hideSupportLink,
		Error:                error,
	}

	renderAdminTemplate(w, templateSetup, data)
}

func (s *httpdServer) renderAddUpdateAdminPage(w http.ResponseWriter, r *http.Request, admin *dataprovider.Admin,
	error string, isAdd bool) {
	groups, err := s.getWebGroups(w, r, defaultQueryLimit, true)
	if err != nil {
		return
	}
	roles, err := s.getWebRoles(w, r, 10, true)
	if err != nil {
		return
	}
	currentURL := webAdminPath
	title := "Add a new admin"
	if !isAdd {
		currentURL = fmt.Sprintf("%v/%v", webAdminPath, url.PathEscape(admin.Username))
		title = "Update admin"
	}
	data := adminPage{
		basePage: s.getBasePageData(title, currentURL, r),
		Admin:    admin,
		Groups:   groups,
		Roles:    roles,
		Error:    error,
		IsAdd:    isAdd,
	}

	renderAdminTemplate(w, templateAdmin, data)
}

func (s *httpdServer) getUserPageTitleAndURL(mode userPageMode, username string) (string, string) {
	var title, currentURL string
	switch mode {
	case userPageModeAdd:
		title = "Add a new user"
		currentURL = webUserPath
	case userPageModeUpdate:
		title = "Update user"
		currentURL = fmt.Sprintf("%v/%v", webUserPath, url.PathEscape(username))
	case userPageModeTemplate:
		title = "User template"
		currentURL = webTemplateUser
	}
	return title, currentURL
}

func (s *httpdServer) renderUserPage(w http.ResponseWriter, r *http.Request, user *dataprovider.User,
	mode userPageMode, errorString string, admin *dataprovider.Admin,
) {
	user.SetEmptySecretsIfNil()
	title, currentURL := s.getUserPageTitleAndURL(mode, user.Username)
	if user.Password != "" && user.IsPasswordHashed() {
		switch mode {
		case userPageModeUpdate:
			user.Password = redactedSecret
		default:
			user.Password = ""
		}
	}
	user.FsConfig.RedactedSecret = redactedSecret
	basePage := s.getBasePageData(title, currentURL, r)
	if (mode == userPageModeAdd || mode == userPageModeTemplate) && len(user.Groups) == 0 && admin != nil {
		for _, group := range admin.Groups {
			user.Groups = append(user.Groups, sdk.GroupMapping{
				Name: group.Name,
				Type: group.Options.GetUserGroupType(),
			})
		}
	}
	var roles []dataprovider.Role
	if basePage.LoggedAdmin.Role == "" {
		var err error
		roles, err = s.getWebRoles(w, r, 10, true)
		if err != nil {
			return
		}
	}
	folders, err := s.getWebVirtualFolders(w, r, defaultQueryLimit, true)
	if err != nil {
		return
	}
	groups, err := s.getWebGroups(w, r, defaultQueryLimit, true)
	if err != nil {
		return
	}
	data := userPage{
		basePage:           basePage,
		Mode:               mode,
		Error:              errorString,
		User:               user,
		ValidPerms:         dataprovider.ValidPerms,
		ValidLoginMethods:  dataprovider.ValidLoginMethods,
		ValidProtocols:     dataprovider.ValidProtocols,
		TwoFactorProtocols: dataprovider.MFAProtocols,
		WebClientOptions:   sdk.WebClientOptions,
		RootDirPerms:       user.GetPermissionsForPath("/"),
		VirtualFolders:     folders,
		Groups:             groups,
		Roles:              roles,
		CanImpersonate:     os.Getuid() == 0,
		FsWrapper: fsWrapper{
			Filesystem:      user.FsConfig,
			IsUserPage:      true,
			IsGroupPage:     false,
			IsHidden:        basePage.LoggedAdmin.Filters.Preferences.HideFilesystem(),
			HasUsersBaseDir: dataprovider.HasUsersBaseDir(),
			DirPath:         user.HomeDir,
		},
	}
	renderAdminTemplate(w, templateUser, data)
}

func (s *httpdServer) renderIPListPage(w http.ResponseWriter, r *http.Request, entry dataprovider.IPListEntry,
	mode genericPageMode, error string,
) {
	var title, currentURL string
	switch mode {
	case genericPageModeAdd:
		title = "Add a new IP List entry"
		currentURL = fmt.Sprintf("%s/%d", webIPListPath, entry.Type)
	case genericPageModeUpdate:
		title = "Update IP List entry"
		currentURL = fmt.Sprintf("%s/%d/%s", webIPListPath, entry.Type, url.PathEscape(entry.IPOrNet))
	}
	data := ipListPage{
		basePage: s.getBasePageData(title, currentURL, r),
		Error:    error,
		Entry:    &entry,
		Mode:     mode,
	}
	renderAdminTemplate(w, templateIPList, data)
}

func (s *httpdServer) renderRolePage(w http.ResponseWriter, r *http.Request, role dataprovider.Role,
	mode genericPageMode, error string,
) {
	var title, currentURL string
	switch mode {
	case genericPageModeAdd:
		title = "Add a new role"
		currentURL = webAdminRolePath
	case genericPageModeUpdate:
		title = "Update role"
		currentURL = fmt.Sprintf("%s/%s", webAdminRolePath, url.PathEscape(role.Name))
	}
	data := rolePage{
		basePage: s.getBasePageData(title, currentURL, r),
		Error:    error,
		Role:     &role,
		Mode:     mode,
	}
	renderAdminTemplate(w, templateRole, data)
}

func (s *httpdServer) renderGroupPage(w http.ResponseWriter, r *http.Request, group dataprovider.Group,
	mode genericPageMode, error string,
) {
	folders, err := s.getWebVirtualFolders(w, r, defaultQueryLimit, true)
	if err != nil {
		return
	}
	group.SetEmptySecretsIfNil()
	group.UserSettings.FsConfig.RedactedSecret = redactedSecret
	var title, currentURL string
	switch mode {
	case genericPageModeAdd:
		title = "Add a new group"
		currentURL = webGroupPath
	case genericPageModeUpdate:
		title = "Update group"
		currentURL = fmt.Sprintf("%v/%v", webGroupPath, url.PathEscape(group.Name))
	}
	group.UserSettings.FsConfig.RedactedSecret = redactedSecret
	group.UserSettings.FsConfig.SetEmptySecretsIfNil()

	data := groupPage{
		basePage:           s.getBasePageData(title, currentURL, r),
		Error:              error,
		Group:              &group,
		Mode:               mode,
		ValidPerms:         dataprovider.ValidPerms,
		ValidLoginMethods:  dataprovider.ValidLoginMethods,
		ValidProtocols:     dataprovider.ValidProtocols,
		TwoFactorProtocols: dataprovider.MFAProtocols,
		WebClientOptions:   sdk.WebClientOptions,
		VirtualFolders:     folders,
		FsWrapper: fsWrapper{
			Filesystem:      group.UserSettings.FsConfig,
			IsUserPage:      false,
			IsGroupPage:     true,
			HasUsersBaseDir: false,
			DirPath:         group.UserSettings.HomeDir,
		},
	}
	renderAdminTemplate(w, templateGroup, data)
}

func (s *httpdServer) renderEventActionPage(w http.ResponseWriter, r *http.Request, action dataprovider.BaseEventAction,
	mode genericPageMode, error string,
) {
	action.Options.SetEmptySecretsIfNil()
	var title, currentURL string
	switch mode {
	case genericPageModeAdd:
		title = "Add a new event action"
		currentURL = webAdminEventActionPath
	case genericPageModeUpdate:
		title = "Update event action"
		currentURL = fmt.Sprintf("%s/%s", webAdminEventActionPath, url.PathEscape(action.Name))
	}
	if action.Options.HTTPConfig.Timeout == 0 {
		action.Options.HTTPConfig.Timeout = 20
	}
	if action.Options.CmdConfig.Timeout == 0 {
		action.Options.CmdConfig.Timeout = 20
	}
	if action.Options.PwdExpirationConfig.Threshold == 0 {
		action.Options.PwdExpirationConfig.Threshold = 10
	}

	data := eventActionPage{
		basePage:       s.getBasePageData(title, currentURL, r),
		Action:         action,
		ActionTypes:    dataprovider.EventActionTypes,
		FsActions:      dataprovider.FsActionTypes,
		HTTPMethods:    dataprovider.SupportedHTTPActionMethods,
		RedactedSecret: redactedSecret,
		Error:          error,
		Mode:           mode,
	}
	renderAdminTemplate(w, templateEventAction, data)
}

func (s *httpdServer) renderEventRulePage(w http.ResponseWriter, r *http.Request, rule dataprovider.EventRule,
	mode genericPageMode, error string,
) {
	actions, err := s.getWebEventActions(w, r, defaultQueryLimit, true)
	if err != nil {
		return
	}
	var title, currentURL string
	switch mode {
	case genericPageModeAdd:
		title = "Add new event rules"
		currentURL = webAdminEventRulePath
	case genericPageModeUpdate:
		title = "Update event rules"
		currentURL = fmt.Sprintf("%v/%v", webAdminEventRulePath, url.PathEscape(rule.Name))
	}

	data := eventRulePage{
		basePage:        s.getBasePageData(title, currentURL, r),
		Rule:            rule,
		TriggerTypes:    dataprovider.EventTriggerTypes,
		Actions:         actions,
		FsEvents:        dataprovider.SupportedFsEvents,
		Protocols:       dataprovider.SupportedRuleConditionProtocols,
		ProviderEvents:  dataprovider.SupportedProviderEvents,
		ProviderObjects: dataprovider.SupporteRuleConditionProviderObjects,
		Error:           error,
		Mode:            mode,
		IsShared:        s.isShared > 0,
	}
	renderAdminTemplate(w, templateEventRule, data)
}

func (s *httpdServer) renderFolderPage(w http.ResponseWriter, r *http.Request, folder vfs.BaseVirtualFolder,
	mode folderPageMode, error string,
) {
	var title, currentURL string
	switch mode {
	case folderPageModeAdd:
		title = "Add a new folder"
		currentURL = webFolderPath
	case folderPageModeUpdate:
		title = "Update folder"
		currentURL = fmt.Sprintf("%v/%v", webFolderPath, url.PathEscape(folder.Name))
	case folderPageModeTemplate:
		title = "Folder template"
		currentURL = webTemplateFolder
	}
	folder.FsConfig.RedactedSecret = redactedSecret
	folder.FsConfig.SetEmptySecretsIfNil()

	data := folderPage{
		basePage: s.getBasePageData(title, currentURL, r),
		Error:    error,
		Folder:   folder,
		Mode:     mode,
		FsWrapper: fsWrapper{
			Filesystem:      folder.FsConfig,
			IsUserPage:      false,
			IsGroupPage:     false,
			HasUsersBaseDir: false,
			DirPath:         folder.MappedPath,
		},
	}
	renderAdminTemplate(w, templateFolder, data)
}

func getFoldersForTemplate(r *http.Request) []string {
	var res []string
	folderNames := r.Form["tpl_foldername"]
	folders := make(map[string]bool)
	for _, name := range folderNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := folders[name]; ok {
			continue
		}
		folders[name] = true
		res = append(res, name)
	}
	return res
}

func getUsersForTemplate(r *http.Request) []userTemplateFields {
	var res []userTemplateFields
	tplUsernames := r.Form["tpl_username"]
	tplPasswords := r.Form["tpl_password"]
	tplPublicKeys := r.Form["tpl_public_keys"]

	users := make(map[string]bool)
	for idx, username := range tplUsernames {
		username = strings.TrimSpace(username)
		password := ""
		publicKey := ""
		if len(tplPasswords) > idx {
			password = strings.TrimSpace(tplPasswords[idx])
		}
		if len(tplPublicKeys) > idx {
			publicKey = strings.TrimSpace(tplPublicKeys[idx])
		}
		if username == "" {
			continue
		}
		if _, ok := users[username]; ok {
			continue
		}

		users[username] = true
		res = append(res, userTemplateFields{
			Username:   username,
			Password:   password,
			PublicKeys: []string{publicKey},
		})
	}

	return res
}

func getVirtualFoldersFromPostFields(r *http.Request) []vfs.VirtualFolder {
	var virtualFolders []vfs.VirtualFolder
	folderPaths := r.Form["vfolder_path"]
	folderNames := r.Form["vfolder_name"]
	folderQuotaSizes := r.Form["vfolder_quota_size"]
	folderQuotaFiles := r.Form["vfolder_quota_files"]
	for idx, p := range folderPaths {
		p = strings.TrimSpace(p)
		name := ""
		if len(folderNames) > idx {
			name = folderNames[idx]
		}
		if p != "" && name != "" {
			vfolder := vfs.VirtualFolder{
				BaseVirtualFolder: vfs.BaseVirtualFolder{
					Name: name,
				},
				VirtualPath: p,
				QuotaFiles:  -1,
				QuotaSize:   -1,
			}
			if len(folderQuotaSizes) > idx {
				quotaSize, err := util.ParseBytes(folderQuotaSizes[idx])
				if err == nil {
					vfolder.QuotaSize = quotaSize
				}
			}
			if len(folderQuotaFiles) > idx {
				quotaFiles, err := strconv.Atoi(strings.TrimSpace(folderQuotaFiles[idx]))
				if err == nil {
					vfolder.QuotaFiles = quotaFiles
				}
			}
			virtualFolders = append(virtualFolders, vfolder)
		}
	}

	return virtualFolders
}

func getSubDirPermissionsFromPostFields(r *http.Request) map[string][]string {
	permissions := make(map[string][]string)

	for k := range r.Form {
		if strings.HasPrefix(k, "sub_perm_path") {
			p := strings.TrimSpace(r.Form.Get(k))
			if p != "" {
				idx := strings.TrimPrefix(k, "sub_perm_path")
				permissions[p] = r.Form[fmt.Sprintf("sub_perm_permissions%v", idx)]
			}
		}
	}

	return permissions
}

func getUserPermissionsFromPostFields(r *http.Request) map[string][]string {
	permissions := getSubDirPermissionsFromPostFields(r)
	permissions["/"] = r.Form["permissions"]

	return permissions
}

func getBandwidthLimitsFromPostFields(r *http.Request) ([]sdk.BandwidthLimit, error) {
	var result []sdk.BandwidthLimit

	for k := range r.Form {
		if strings.HasPrefix(k, "bandwidth_limit_sources") {
			sources := getSliceFromDelimitedValues(r.Form.Get(k), ",")
			if len(sources) > 0 {
				bwLimit := sdk.BandwidthLimit{
					Sources: sources,
				}
				idx := strings.TrimPrefix(k, "bandwidth_limit_sources")
				ul := r.Form.Get(fmt.Sprintf("upload_bandwidth_source%v", idx))
				dl := r.Form.Get(fmt.Sprintf("download_bandwidth_source%v", idx))
				if ul != "" {
					bandwidthUL, err := strconv.ParseInt(ul, 10, 64)
					if err != nil {
						return result, fmt.Errorf("invalid upload_bandwidth_source%v %q: %w", idx, ul, err)
					}
					bwLimit.UploadBandwidth = bandwidthUL
				}
				if dl != "" {
					bandwidthDL, err := strconv.ParseInt(dl, 10, 64)
					if err != nil {
						return result, fmt.Errorf("invalid download_bandwidth_source%v %q: %w", idx, ul, err)
					}
					bwLimit.DownloadBandwidth = bandwidthDL
				}
				result = append(result, bwLimit)
			}
		}
	}

	return result, nil
}

func getPatterDenyPolicyFromString(policy string) int {
	denyPolicy := sdk.DenyPolicyDefault
	if policy == "1" {
		denyPolicy = sdk.DenyPolicyHide
	}
	return denyPolicy
}

func getFilePatternsFromPostField(r *http.Request) []sdk.PatternsFilter {
	var result []sdk.PatternsFilter

	allowedPatterns := make(map[string][]string)
	deniedPatterns := make(map[string][]string)
	patternPolicies := make(map[string]string)

	for k := range r.Form {
		if strings.HasPrefix(k, "pattern_path") {
			p := strings.TrimSpace(r.Form.Get(k))
			idx := strings.TrimPrefix(k, "pattern_path")
			filters := strings.TrimSpace(r.Form.Get(fmt.Sprintf("patterns%v", idx)))
			filters = strings.ReplaceAll(filters, " ", "")
			patternType := r.Form.Get(fmt.Sprintf("pattern_type%v", idx))
			patternPolicy := r.Form.Get(fmt.Sprintf("pattern_policy%v", idx))
			if p != "" && filters != "" {
				if patternType == "allowed" {
					allowedPatterns[p] = append(allowedPatterns[p], strings.Split(filters, ",")...)
				} else {
					deniedPatterns[p] = append(deniedPatterns[p], strings.Split(filters, ",")...)
				}
				if patternPolicy != "" && patternPolicy != "0" {
					patternPolicies[p] = patternPolicy
				}
			}
		}
	}

	for dirAllowed, allowPatterns := range allowedPatterns {
		filter := sdk.PatternsFilter{
			Path:            dirAllowed,
			AllowedPatterns: allowPatterns,
			DenyPolicy:      getPatterDenyPolicyFromString(patternPolicies[dirAllowed]),
		}
		for dirDenied, denPatterns := range deniedPatterns {
			if dirAllowed == dirDenied {
				filter.DeniedPatterns = denPatterns
				break
			}
		}
		result = append(result, filter)
	}
	for dirDenied, denPatterns := range deniedPatterns {
		found := false
		for _, res := range result {
			if res.Path == dirDenied {
				found = true
				break
			}
		}
		if !found {
			result = append(result, sdk.PatternsFilter{
				Path:           dirDenied,
				DeniedPatterns: denPatterns,
				DenyPolicy:     getPatterDenyPolicyFromString(patternPolicies[dirDenied]),
			})
		}
	}
	return result
}

func getGroupsFromUserPostFields(r *http.Request) []sdk.GroupMapping {
	var groups []sdk.GroupMapping

	primaryGroup := strings.TrimSpace(r.Form.Get("primary_group"))
	if primaryGroup != "" {
		groups = append(groups, sdk.GroupMapping{
			Name: primaryGroup,
			Type: sdk.GroupTypePrimary,
		})
	}
	secondaryGroups := r.Form["secondary_groups"]
	for _, name := range secondaryGroups {
		groups = append(groups, sdk.GroupMapping{
			Name: strings.TrimSpace(name),
			Type: sdk.GroupTypeSecondary,
		})
	}
	membershipGroups := r.Form["membership_groups"]
	for _, name := range membershipGroups {
		groups = append(groups, sdk.GroupMapping{
			Name: strings.TrimSpace(name),
			Type: sdk.GroupTypeMembership,
		})
	}
	return groups
}

func getFiltersFromUserPostFields(r *http.Request) (sdk.BaseUserFilters, error) {
	var filters sdk.BaseUserFilters
	bwLimits, err := getBandwidthLimitsFromPostFields(r)
	if err != nil {
		return filters, err
	}
	maxFileSize, err := util.ParseBytes(r.Form.Get("max_upload_file_size"))
	if err != nil {
		return filters, fmt.Errorf("invalid max upload file size: %w", err)
	}
	defaultSharesExpiration, err := strconv.Atoi(r.Form.Get("default_shares_expiration"))
	if err != nil {
		return filters, fmt.Errorf("invalid default shares expiration: %w", err)
	}
	passwordExpiration, err := strconv.Atoi(r.Form.Get("password_expiration"))
	if err != nil {
		return filters, fmt.Errorf("invalid password expiration: %w", err)
	}
	passwordStrength, err := strconv.Atoi(r.Form.Get("password_strength"))
	if err != nil {
		return filters, fmt.Errorf("invalid password strength: %w", err)
	}
	if r.Form.Get("ftp_security") == "1" {
		filters.FTPSecurity = 1
	}
	filters.BandwidthLimits = bwLimits
	filters.AllowedIP = getSliceFromDelimitedValues(r.Form.Get("allowed_ip"), ",")
	filters.DeniedIP = getSliceFromDelimitedValues(r.Form.Get("denied_ip"), ",")
	filters.DeniedLoginMethods = r.Form["denied_login_methods"]
	filters.DeniedProtocols = r.Form["denied_protocols"]
	filters.TwoFactorAuthProtocols = r.Form["required_two_factor_protocols"]
	filters.FilePatterns = getFilePatternsFromPostField(r)
	filters.TLSUsername = sdk.TLSUsername(strings.TrimSpace(r.Form.Get("tls_username")))
	filters.WebClient = r.Form["web_client_options"]
	filters.DefaultSharesExpiration = defaultSharesExpiration
	filters.PasswordExpiration = passwordExpiration
	filters.PasswordStrength = passwordStrength
	hooks := r.Form["hooks"]
	if util.Contains(hooks, "external_auth_disabled") {
		filters.Hooks.ExternalAuthDisabled = true
	}
	if util.Contains(hooks, "pre_login_disabled") {
		filters.Hooks.PreLoginDisabled = true
	}
	if util.Contains(hooks, "check_password_disabled") {
		filters.Hooks.CheckPasswordDisabled = true
	}
	filters.IsAnonymous = r.Form.Get("is_anonymous") != ""
	filters.DisableFsChecks = r.Form.Get("disable_fs_checks") != ""
	filters.AllowAPIKeyAuth = r.Form.Get("allow_api_key_auth") != ""
	filters.StartDirectory = strings.TrimSpace(r.Form.Get("start_directory"))
	filters.MaxUploadFileSize = maxFileSize
	filters.ExternalAuthCacheTime, err = strconv.ParseInt(r.Form.Get("external_auth_cache_time"), 10, 64)
	if err != nil {
		return filters, fmt.Errorf("invalid external auth cache time: %w", err)
	}
	return filters, nil
}

func getSecretFromFormField(r *http.Request, field string) *kms.Secret {
	secret := kms.NewPlainSecret(r.Form.Get(field))
	if strings.TrimSpace(secret.GetPayload()) == redactedSecret {
		secret.SetStatus(sdkkms.SecretStatusRedacted)
	}
	if strings.TrimSpace(secret.GetPayload()) == "" {
		secret.SetStatus("")
	}
	return secret
}

func getS3Config(r *http.Request) (vfs.S3FsConfig, error) {
	var err error
	config := vfs.S3FsConfig{}
	config.Bucket = strings.TrimSpace(r.Form.Get("s3_bucket"))
	config.Region = strings.TrimSpace(r.Form.Get("s3_region"))
	config.AccessKey = strings.TrimSpace(r.Form.Get("s3_access_key"))
	config.RoleARN = strings.TrimSpace(r.Form.Get("s3_role_arn"))
	config.AccessSecret = getSecretFromFormField(r, "s3_access_secret")
	config.Endpoint = strings.TrimSpace(r.Form.Get("s3_endpoint"))
	config.StorageClass = strings.TrimSpace(r.Form.Get("s3_storage_class"))
	config.ACL = strings.TrimSpace(r.Form.Get("s3_acl"))
	config.KeyPrefix = strings.TrimSpace(strings.TrimPrefix(r.Form.Get("s3_key_prefix"), "/"))
	config.UploadPartSize, err = strconv.ParseInt(r.Form.Get("s3_upload_part_size"), 10, 64)
	if err != nil {
		return config, fmt.Errorf("invalid s3 upload part size: %w", err)
	}
	config.UploadConcurrency, err = strconv.Atoi(r.Form.Get("s3_upload_concurrency"))
	if err != nil {
		return config, fmt.Errorf("invalid s3 upload concurrency: %w", err)
	}
	config.DownloadPartSize, err = strconv.ParseInt(r.Form.Get("s3_download_part_size"), 10, 64)
	if err != nil {
		return config, fmt.Errorf("invalid s3 download part size: %w", err)
	}
	config.DownloadConcurrency, err = strconv.Atoi(r.Form.Get("s3_download_concurrency"))
	if err != nil {
		return config, fmt.Errorf("invalid s3 download concurrency: %w", err)
	}
	config.ForcePathStyle = r.Form.Get("s3_force_path_style") != ""
	config.DownloadPartMaxTime, err = strconv.Atoi(r.Form.Get("s3_download_part_max_time"))
	if err != nil {
		return config, fmt.Errorf("invalid s3 download part max time: %w", err)
	}
	config.UploadPartMaxTime, err = strconv.Atoi(r.Form.Get("s3_upload_part_max_time"))
	if err != nil {
		return config, fmt.Errorf("invalid s3 upload part max time: %w", err)
	}
	return config, nil
}

func getGCSConfig(r *http.Request) (vfs.GCSFsConfig, error) {
	var err error
	config := vfs.GCSFsConfig{}

	config.Bucket = strings.TrimSpace(r.Form.Get("gcs_bucket"))
	config.StorageClass = strings.TrimSpace(r.Form.Get("gcs_storage_class"))
	config.ACL = strings.TrimSpace(r.Form.Get("gcs_acl"))
	config.KeyPrefix = strings.TrimSpace(strings.TrimPrefix(r.Form.Get("gcs_key_prefix"), "/"))
	uploadPartSize, err := strconv.ParseInt(r.Form.Get("gcs_upload_part_size"), 10, 64)
	if err == nil {
		config.UploadPartSize = uploadPartSize
	}
	uploadPartMaxTime, err := strconv.Atoi(r.Form.Get("gcs_upload_part_max_time"))
	if err == nil {
		config.UploadPartMaxTime = uploadPartMaxTime
	}
	autoCredentials := r.Form.Get("gcs_auto_credentials")
	if autoCredentials != "" {
		config.AutomaticCredentials = 1
	} else {
		config.AutomaticCredentials = 0
	}
	credentials, _, err := r.FormFile("gcs_credential_file")
	if err == http.ErrMissingFile {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	defer credentials.Close()
	fileBytes, err := io.ReadAll(credentials)
	if err != nil || len(fileBytes) == 0 {
		if len(fileBytes) == 0 {
			err = errors.New("credentials file size must be greater than 0")
		}
		return config, err
	}
	config.Credentials = kms.NewPlainSecret(string(fileBytes))
	config.AutomaticCredentials = 0
	return config, err
}

func getSFTPConfig(r *http.Request) (vfs.SFTPFsConfig, error) {
	var err error
	config := vfs.SFTPFsConfig{}
	config.Endpoint = strings.TrimSpace(r.Form.Get("sftp_endpoint"))
	config.Username = strings.TrimSpace(r.Form.Get("sftp_username"))
	config.Password = getSecretFromFormField(r, "sftp_password")
	config.PrivateKey = getSecretFromFormField(r, "sftp_private_key")
	config.KeyPassphrase = getSecretFromFormField(r, "sftp_key_passphrase")
	fingerprintsFormValue := r.Form.Get("sftp_fingerprints")
	config.Fingerprints = getSliceFromDelimitedValues(fingerprintsFormValue, "\n")
	config.Prefix = strings.TrimSpace(r.Form.Get("sftp_prefix"))
	config.DisableCouncurrentReads = r.Form.Get("sftp_disable_concurrent_reads") != ""
	config.BufferSize, err = strconv.ParseInt(r.Form.Get("sftp_buffer_size"), 10, 64)
	if r.Form.Get("sftp_equality_check_mode") != "" {
		config.EqualityCheckMode = 1
	} else {
		config.EqualityCheckMode = 0
	}
	if err != nil {
		return config, fmt.Errorf("invalid SFTP buffer size: %w", err)
	}
	return config, nil
}

func getHTTPFsConfig(r *http.Request) vfs.HTTPFsConfig {
	config := vfs.HTTPFsConfig{}
	config.Endpoint = strings.TrimSpace(r.Form.Get("http_endpoint"))
	config.Username = strings.TrimSpace(r.Form.Get("http_username"))
	config.SkipTLSVerify = r.Form.Get("http_skip_tls_verify") != ""
	config.Password = getSecretFromFormField(r, "http_password")
	config.APIKey = getSecretFromFormField(r, "http_api_key")
	if r.Form.Get("http_equality_check_mode") != "" {
		config.EqualityCheckMode = 1
	} else {
		config.EqualityCheckMode = 0
	}
	return config
}

func getAzureConfig(r *http.Request) (vfs.AzBlobFsConfig, error) {
	var err error
	config := vfs.AzBlobFsConfig{}
	config.Container = strings.TrimSpace(r.Form.Get("az_container"))
	config.AccountName = strings.TrimSpace(r.Form.Get("az_account_name"))
	config.AccountKey = getSecretFromFormField(r, "az_account_key")
	config.SASURL = getSecretFromFormField(r, "az_sas_url")
	config.Endpoint = strings.TrimSpace(r.Form.Get("az_endpoint"))
	config.KeyPrefix = strings.TrimSpace(strings.TrimPrefix(r.Form.Get("az_key_prefix"), "/"))
	config.AccessTier = strings.TrimSpace(r.Form.Get("az_access_tier"))
	config.UseEmulator = r.Form.Get("az_use_emulator") != ""
	config.UploadPartSize, err = strconv.ParseInt(r.Form.Get("az_upload_part_size"), 10, 64)
	if err != nil {
		return config, fmt.Errorf("invalid azure upload part size: %w", err)
	}
	config.UploadConcurrency, err = strconv.Atoi(r.Form.Get("az_upload_concurrency"))
	if err != nil {
		return config, fmt.Errorf("invalid azure upload concurrency: %w", err)
	}
	config.DownloadPartSize, err = strconv.ParseInt(r.Form.Get("az_download_part_size"), 10, 64)
	if err != nil {
		return config, fmt.Errorf("invalid azure download part size: %w", err)
	}
	config.DownloadConcurrency, err = strconv.Atoi(r.Form.Get("az_download_concurrency"))
	if err != nil {
		return config, fmt.Errorf("invalid azure download concurrency: %w", err)
	}
	return config, nil
}

func getOsConfigFromPostFields(r *http.Request, readBufferField, writeBufferField string) sdk.OSFsConfig {
	config := sdk.OSFsConfig{}
	readBuffer, err := strconv.Atoi(r.Form.Get(readBufferField))
	if err == nil {
		config.ReadBufferSize = readBuffer
	}
	writeBuffer, err := strconv.Atoi(r.Form.Get(writeBufferField))
	if err == nil {
		config.WriteBufferSize = writeBuffer
	}
	return config
}

func getFsConfigFromPostFields(r *http.Request) (vfs.Filesystem, error) {
	var fs vfs.Filesystem
	fs.Provider = sdk.GetProviderByName(r.Form.Get("fs_provider"))
	switch fs.Provider {
	case sdk.LocalFilesystemProvider:
		fs.OSConfig = getOsConfigFromPostFields(r, "osfs_read_buffer_size", "osfs_write_buffer_size")
	case sdk.S3FilesystemProvider:
		config, err := getS3Config(r)
		if err != nil {
			return fs, err
		}
		fs.S3Config = config
	case sdk.AzureBlobFilesystemProvider:
		config, err := getAzureConfig(r)
		if err != nil {
			return fs, err
		}
		fs.AzBlobConfig = config
	case sdk.GCSFilesystemProvider:
		config, err := getGCSConfig(r)
		if err != nil {
			return fs, err
		}
		fs.GCSConfig = config
	case sdk.CryptedFilesystemProvider:
		fs.CryptConfig.Passphrase = getSecretFromFormField(r, "crypt_passphrase")
		fs.CryptConfig.OSFsConfig = getOsConfigFromPostFields(r, "cryptfs_read_buffer_size", "cryptfs_write_buffer_size")
	case sdk.SFTPFilesystemProvider:
		config, err := getSFTPConfig(r)
		if err != nil {
			return fs, err
		}
		fs.SFTPConfig = config
	case sdk.HTTPFilesystemProvider:
		fs.HTTPConfig = getHTTPFsConfig(r)
	}
	return fs, nil
}

func getAdminHiddenUserPageSections(r *http.Request) int {
	var result int

	for _, val := range r.Form["user_page_hidden_sections"] {
		switch val {
		case "1":
			result++
		case "2":
			result += 2
		case "3":
			result += 4
		case "4":
			result += 8
		case "5":
			result += 16
		case "6":
			result += 32
		case "7":
			result += 64
		}
	}

	return result
}

func getAdminFromPostFields(r *http.Request) (dataprovider.Admin, error) {
	var admin dataprovider.Admin
	err := r.ParseForm()
	if err != nil {
		return admin, err
	}
	status, err := strconv.Atoi(r.Form.Get("status"))
	if err != nil {
		return admin, fmt.Errorf("invalid status: %w", err)
	}
	admin.Username = strings.TrimSpace(r.Form.Get("username"))
	admin.Password = strings.TrimSpace(r.Form.Get("password"))
	admin.Permissions = r.Form["permissions"]
	admin.Email = strings.TrimSpace(r.Form.Get("email"))
	admin.Status = status
	admin.Role = strings.TrimSpace(r.Form.Get("role"))
	admin.Filters.AllowList = getSliceFromDelimitedValues(r.Form.Get("allowed_ip"), ",")
	admin.Filters.AllowAPIKeyAuth = r.Form.Get("allow_api_key_auth") != ""
	admin.AdditionalInfo = r.Form.Get("additional_info")
	admin.Description = r.Form.Get("description")
	admin.Filters.Preferences.HideUserPageSections = getAdminHiddenUserPageSections(r)
	admin.Filters.Preferences.DefaultUsersExpiration = 0
	if val := r.Form.Get("default_users_expiration"); val != "" {
		defaultUsersExpiration, err := strconv.Atoi(r.Form.Get("default_users_expiration"))
		if err != nil {
			return admin, fmt.Errorf("invalid default users expiration: %w", err)
		}
		admin.Filters.Preferences.DefaultUsersExpiration = defaultUsersExpiration
	}
	for k := range r.Form {
		if strings.HasPrefix(k, "group") {
			groupName := strings.TrimSpace(r.Form.Get(k))
			if groupName != "" {
				idx := strings.TrimPrefix(k, "group")
				addAsGroupType := r.Form.Get(fmt.Sprintf("add_as_group_type%s", idx))
				group := dataprovider.AdminGroupMapping{
					Name: groupName,
				}
				switch addAsGroupType {
				case "1":
					group.Options.AddToUsersAs = dataprovider.GroupAddToUsersAsPrimary
				case "2":
					group.Options.AddToUsersAs = dataprovider.GroupAddToUsersAsSecondary
				default:
					group.Options.AddToUsersAs = dataprovider.GroupAddToUsersAsMembership
				}
				admin.Groups = append(admin.Groups, group)
			}
		}
	}
	return admin, nil
}

func replacePlaceholders(field string, replacements map[string]string) string {
	for k, v := range replacements {
		field = strings.ReplaceAll(field, k, v)
	}
	return field
}

func getFolderFromTemplate(folder vfs.BaseVirtualFolder, name string) vfs.BaseVirtualFolder {
	folder.Name = name
	replacements := make(map[string]string)
	replacements["%name%"] = folder.Name

	folder.MappedPath = replacePlaceholders(folder.MappedPath, replacements)
	folder.Description = replacePlaceholders(folder.Description, replacements)
	switch folder.FsConfig.Provider {
	case sdk.CryptedFilesystemProvider:
		folder.FsConfig.CryptConfig = getCryptFsFromTemplate(folder.FsConfig.CryptConfig, replacements)
	case sdk.S3FilesystemProvider:
		folder.FsConfig.S3Config = getS3FsFromTemplate(folder.FsConfig.S3Config, replacements)
	case sdk.GCSFilesystemProvider:
		folder.FsConfig.GCSConfig = getGCSFsFromTemplate(folder.FsConfig.GCSConfig, replacements)
	case sdk.AzureBlobFilesystemProvider:
		folder.FsConfig.AzBlobConfig = getAzBlobFsFromTemplate(folder.FsConfig.AzBlobConfig, replacements)
	case sdk.SFTPFilesystemProvider:
		folder.FsConfig.SFTPConfig = getSFTPFsFromTemplate(folder.FsConfig.SFTPConfig, replacements)
	case sdk.HTTPFilesystemProvider:
		folder.FsConfig.HTTPConfig = getHTTPFsFromTemplate(folder.FsConfig.HTTPConfig, replacements)
	}

	return folder
}

func getCryptFsFromTemplate(fsConfig vfs.CryptFsConfig, replacements map[string]string) vfs.CryptFsConfig {
	if fsConfig.Passphrase != nil {
		if fsConfig.Passphrase.IsPlain() {
			payload := replacePlaceholders(fsConfig.Passphrase.GetPayload(), replacements)
			fsConfig.Passphrase = kms.NewPlainSecret(payload)
		}
	}
	return fsConfig
}

func getS3FsFromTemplate(fsConfig vfs.S3FsConfig, replacements map[string]string) vfs.S3FsConfig {
	fsConfig.KeyPrefix = replacePlaceholders(fsConfig.KeyPrefix, replacements)
	fsConfig.AccessKey = replacePlaceholders(fsConfig.AccessKey, replacements)
	if fsConfig.AccessSecret != nil && fsConfig.AccessSecret.IsPlain() {
		payload := replacePlaceholders(fsConfig.AccessSecret.GetPayload(), replacements)
		fsConfig.AccessSecret = kms.NewPlainSecret(payload)
	}
	return fsConfig
}

func getGCSFsFromTemplate(fsConfig vfs.GCSFsConfig, replacements map[string]string) vfs.GCSFsConfig {
	fsConfig.KeyPrefix = replacePlaceholders(fsConfig.KeyPrefix, replacements)
	return fsConfig
}

func getAzBlobFsFromTemplate(fsConfig vfs.AzBlobFsConfig, replacements map[string]string) vfs.AzBlobFsConfig {
	fsConfig.KeyPrefix = replacePlaceholders(fsConfig.KeyPrefix, replacements)
	fsConfig.AccountName = replacePlaceholders(fsConfig.AccountName, replacements)
	if fsConfig.AccountKey != nil && fsConfig.AccountKey.IsPlain() {
		payload := replacePlaceholders(fsConfig.AccountKey.GetPayload(), replacements)
		fsConfig.AccountKey = kms.NewPlainSecret(payload)
	}
	return fsConfig
}

func getSFTPFsFromTemplate(fsConfig vfs.SFTPFsConfig, replacements map[string]string) vfs.SFTPFsConfig {
	fsConfig.Prefix = replacePlaceholders(fsConfig.Prefix, replacements)
	fsConfig.Username = replacePlaceholders(fsConfig.Username, replacements)
	if fsConfig.Password != nil && fsConfig.Password.IsPlain() {
		payload := replacePlaceholders(fsConfig.Password.GetPayload(), replacements)
		fsConfig.Password = kms.NewPlainSecret(payload)
	}
	return fsConfig
}

func getHTTPFsFromTemplate(fsConfig vfs.HTTPFsConfig, replacements map[string]string) vfs.HTTPFsConfig {
	fsConfig.Username = replacePlaceholders(fsConfig.Username, replacements)
	return fsConfig
}

func getUserFromTemplate(user dataprovider.User, template userTemplateFields) dataprovider.User {
	user.Username = template.Username
	user.Password = template.Password
	user.PublicKeys = template.PublicKeys
	replacements := make(map[string]string)
	replacements["%username%"] = user.Username
	if user.Password != "" && !user.IsPasswordHashed() {
		user.Password = replacePlaceholders(user.Password, replacements)
		replacements["%password%"] = user.Password
	}

	user.HomeDir = replacePlaceholders(user.HomeDir, replacements)
	var vfolders []vfs.VirtualFolder
	for _, vfolder := range user.VirtualFolders {
		vfolder.Name = replacePlaceholders(vfolder.Name, replacements)
		vfolder.VirtualPath = replacePlaceholders(vfolder.VirtualPath, replacements)
		vfolders = append(vfolders, vfolder)
	}
	user.VirtualFolders = vfolders
	user.Description = replacePlaceholders(user.Description, replacements)
	user.AdditionalInfo = replacePlaceholders(user.AdditionalInfo, replacements)
	user.Filters.StartDirectory = replacePlaceholders(user.Filters.StartDirectory, replacements)

	switch user.FsConfig.Provider {
	case sdk.CryptedFilesystemProvider:
		user.FsConfig.CryptConfig = getCryptFsFromTemplate(user.FsConfig.CryptConfig, replacements)
	case sdk.S3FilesystemProvider:
		user.FsConfig.S3Config = getS3FsFromTemplate(user.FsConfig.S3Config, replacements)
	case sdk.GCSFilesystemProvider:
		user.FsConfig.GCSConfig = getGCSFsFromTemplate(user.FsConfig.GCSConfig, replacements)
	case sdk.AzureBlobFilesystemProvider:
		user.FsConfig.AzBlobConfig = getAzBlobFsFromTemplate(user.FsConfig.AzBlobConfig, replacements)
	case sdk.SFTPFilesystemProvider:
		user.FsConfig.SFTPConfig = getSFTPFsFromTemplate(user.FsConfig.SFTPConfig, replacements)
	case sdk.HTTPFilesystemProvider:
		user.FsConfig.HTTPConfig = getHTTPFsFromTemplate(user.FsConfig.HTTPConfig, replacements)
	}

	return user
}

func getTransferLimits(r *http.Request) (int64, int64, int64, error) {
	dataTransferUL, err := strconv.ParseInt(r.Form.Get("upload_data_transfer"), 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid upload data transfer: %w", err)
	}
	dataTransferDL, err := strconv.ParseInt(r.Form.Get("download_data_transfer"), 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid download data transfer: %w", err)
	}
	dataTransferTotal, err := strconv.ParseInt(r.Form.Get("total_data_transfer"), 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid total data transfer: %w", err)
	}
	return dataTransferUL, dataTransferDL, dataTransferTotal, nil
}

func getQuotaLimits(r *http.Request) (int64, int, error) {
	quotaSize, err := util.ParseBytes(r.Form.Get("quota_size"))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid quota size: %w", err)
	}
	quotaFiles, err := strconv.Atoi(r.Form.Get("quota_files"))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid quota files: %w", err)
	}
	return quotaSize, quotaFiles, nil
}

func getUserFromPostFields(r *http.Request) (dataprovider.User, error) {
	user := dataprovider.User{}
	err := r.ParseMultipartForm(maxRequestSize)
	if err != nil {
		return user, err
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck
	uid, err := strconv.Atoi(r.Form.Get("uid"))
	if err != nil {
		return user, fmt.Errorf("invalid uid: %w", err)
	}
	gid, err := strconv.Atoi(r.Form.Get("gid"))
	if err != nil {
		return user, fmt.Errorf("invalid uid: %w", err)
	}
	maxSessions, err := strconv.Atoi(r.Form.Get("max_sessions"))
	if err != nil {
		return user, fmt.Errorf("invalid max sessions: %w", err)
	}
	quotaSize, quotaFiles, err := getQuotaLimits(r)
	if err != nil {
		return user, err
	}
	bandwidthUL, err := strconv.ParseInt(r.Form.Get("upload_bandwidth"), 10, 64)
	if err != nil {
		return user, fmt.Errorf("invalid upload bandwidth: %w", err)
	}
	bandwidthDL, err := strconv.ParseInt(r.Form.Get("download_bandwidth"), 10, 64)
	if err != nil {
		return user, fmt.Errorf("invalid download bandwidth: %w", err)
	}
	dataTransferUL, dataTransferDL, dataTransferTotal, err := getTransferLimits(r)
	if err != nil {
		return user, err
	}
	status, err := strconv.Atoi(r.Form.Get("status"))
	if err != nil {
		return user, fmt.Errorf("invalid status: %w", err)
	}
	expirationDateMillis := int64(0)
	expirationDateString := r.Form.Get("expiration_date")
	if strings.TrimSpace(expirationDateString) != "" {
		expirationDate, err := time.Parse(webDateTimeFormat, expirationDateString)
		if err != nil {
			return user, err
		}
		expirationDateMillis = util.GetTimeAsMsSinceEpoch(expirationDate)
	}
	fsConfig, err := getFsConfigFromPostFields(r)
	if err != nil {
		return user, err
	}
	filters, err := getFiltersFromUserPostFields(r)
	if err != nil {
		return user, err
	}
	user = dataprovider.User{
		BaseUser: sdk.BaseUser{
			Username:             strings.TrimSpace(r.Form.Get("username")),
			Email:                strings.TrimSpace(r.Form.Get("email")),
			Password:             strings.TrimSpace(r.Form.Get("password")),
			PublicKeys:           r.Form["public_keys"],
			HomeDir:              strings.TrimSpace(r.Form.Get("home_dir")),
			UID:                  uid,
			GID:                  gid,
			Permissions:          getUserPermissionsFromPostFields(r),
			MaxSessions:          maxSessions,
			QuotaSize:            quotaSize,
			QuotaFiles:           quotaFiles,
			UploadBandwidth:      bandwidthUL,
			DownloadBandwidth:    bandwidthDL,
			UploadDataTransfer:   dataTransferUL,
			DownloadDataTransfer: dataTransferDL,
			TotalDataTransfer:    dataTransferTotal,
			Status:               status,
			ExpirationDate:       expirationDateMillis,
			AdditionalInfo:       r.Form.Get("additional_info"),
			Description:          r.Form.Get("description"),
			Role:                 strings.TrimSpace(r.Form.Get("role")),
		},
		Filters: dataprovider.UserFilters{
			BaseUserFilters:       filters,
			RequirePasswordChange: r.Form.Get("require_password_change") != "",
		},
		VirtualFolders: getVirtualFoldersFromPostFields(r),
		FsConfig:       fsConfig,
		Groups:         getGroupsFromUserPostFields(r),
	}
	return user, nil
}

func getGroupFromPostFields(r *http.Request) (dataprovider.Group, error) {
	group := dataprovider.Group{}
	err := r.ParseMultipartForm(maxRequestSize)
	if err != nil {
		return group, err
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	maxSessions, err := strconv.Atoi(r.Form.Get("max_sessions"))
	if err != nil {
		return group, fmt.Errorf("invalid max sessions: %w", err)
	}
	quotaSize, quotaFiles, err := getQuotaLimits(r)
	if err != nil {
		return group, err
	}
	bandwidthUL, err := strconv.ParseInt(r.Form.Get("upload_bandwidth"), 10, 64)
	if err != nil {
		return group, fmt.Errorf("invalid upload bandwidth: %w", err)
	}
	bandwidthDL, err := strconv.ParseInt(r.Form.Get("download_bandwidth"), 10, 64)
	if err != nil {
		return group, fmt.Errorf("invalid download bandwidth: %w", err)
	}
	dataTransferUL, dataTransferDL, dataTransferTotal, err := getTransferLimits(r)
	if err != nil {
		return group, err
	}
	expiresIn, err := strconv.Atoi(r.Form.Get("expires_in"))
	if err != nil {
		return group, fmt.Errorf("invalid expires in: %w", err)
	}
	fsConfig, err := getFsConfigFromPostFields(r)
	if err != nil {
		return group, err
	}
	filters, err := getFiltersFromUserPostFields(r)
	if err != nil {
		return group, err
	}
	group = dataprovider.Group{
		BaseGroup: sdk.BaseGroup{
			Name:        strings.TrimSpace(r.Form.Get("name")),
			Description: r.Form.Get("description"),
		},
		UserSettings: dataprovider.GroupUserSettings{
			BaseGroupUserSettings: sdk.BaseGroupUserSettings{
				HomeDir:              strings.TrimSpace(r.Form.Get("home_dir")),
				MaxSessions:          maxSessions,
				QuotaSize:            quotaSize,
				QuotaFiles:           quotaFiles,
				Permissions:          getSubDirPermissionsFromPostFields(r),
				UploadBandwidth:      bandwidthUL,
				DownloadBandwidth:    bandwidthDL,
				UploadDataTransfer:   dataTransferUL,
				DownloadDataTransfer: dataTransferDL,
				TotalDataTransfer:    dataTransferTotal,
				ExpiresIn:            expiresIn,
				Filters:              filters,
			},
			FsConfig: fsConfig,
		},
		VirtualFolders: getVirtualFoldersFromPostFields(r),
	}
	return group, nil
}

func getKeyValsFromPostFields(r *http.Request, key, val string) []dataprovider.KeyValue {
	var res []dataprovider.KeyValue
	for k := range r.Form {
		if strings.HasPrefix(k, key) {
			formKey := r.Form.Get(k)
			idx := strings.TrimPrefix(k, key)
			formVal := strings.TrimSpace(r.Form.Get(fmt.Sprintf("%s%s", val, idx)))
			if formKey != "" && formVal != "" {
				res = append(res, dataprovider.KeyValue{
					Key:   formKey,
					Value: formVal,
				})
			}
		}
	}
	return res
}

func getFoldersRetentionFromPostFields(r *http.Request) ([]dataprovider.FolderRetention, error) {
	var res []dataprovider.FolderRetention
	for k := range r.Form {
		if strings.HasPrefix(k, "folder_retention_path") {
			folderPath := strings.TrimSpace(r.Form.Get(k))
			if folderPath != "" {
				idx := strings.TrimPrefix(k, "folder_retention_path")
				retention, err := strconv.Atoi(r.Form.Get(fmt.Sprintf("folder_retention_val%s", idx)))
				if err != nil {
					return nil, fmt.Errorf("invalid retention for path %q: %w", folderPath, err)
				}
				options := r.Form[fmt.Sprintf("folder_retention_options%s", idx)]
				res = append(res, dataprovider.FolderRetention{
					Path:                  folderPath,
					Retention:             retention,
					DeleteEmptyDirs:       util.Contains(options, "1"),
					IgnoreUserPermissions: util.Contains(options, "2"),
				})
			}
		}
	}
	return res, nil
}

func getHTTPPartsFromPostFields(r *http.Request) []dataprovider.HTTPPart {
	var result []dataprovider.HTTPPart
	for k := range r.Form {
		if strings.HasPrefix(k, "http_part_name") {
			partName := strings.TrimSpace(r.Form.Get(k))
			if partName != "" {
				idx := strings.TrimPrefix(k, "http_part_name")
				order, err := strconv.Atoi(idx)
				if err != nil {
					continue
				}
				filePath := strings.TrimSpace(r.Form.Get(fmt.Sprintf("http_part_file%s", idx)))
				body := r.Form.Get(fmt.Sprintf("http_part_body%s", idx))
				concatHeaders := getSliceFromDelimitedValues(r.Form.Get(fmt.Sprintf("http_part_headers%s", idx)), "\n")
				var headers []dataprovider.KeyValue
				for _, h := range concatHeaders {
					values := strings.SplitN(h, ":", 2)
					if len(values) > 1 {
						headers = append(headers, dataprovider.KeyValue{
							Key:   strings.TrimSpace(values[0]),
							Value: strings.TrimSpace(values[1]),
						})
					}
				}
				result = append(result, dataprovider.HTTPPart{
					Name:     partName,
					Filepath: filePath,
					Headers:  headers,
					Body:     body,
					Order:    order,
				})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})
	return result
}

func getEventActionOptionsFromPostFields(r *http.Request) (dataprovider.BaseEventActionOptions, error) {
	httpTimeout, err := strconv.Atoi(r.Form.Get("http_timeout"))
	if err != nil {
		return dataprovider.BaseEventActionOptions{}, fmt.Errorf("invalid http timeout: %w", err)
	}
	cmdTimeout, err := strconv.Atoi(r.Form.Get("cmd_timeout"))
	if err != nil {
		return dataprovider.BaseEventActionOptions{}, fmt.Errorf("invalid command timeout: %w", err)
	}
	foldersRetention, err := getFoldersRetentionFromPostFields(r)
	if err != nil {
		return dataprovider.BaseEventActionOptions{}, err
	}
	fsActionType, err := strconv.Atoi(r.Form.Get("fs_action_type"))
	if err != nil {
		return dataprovider.BaseEventActionOptions{}, fmt.Errorf("invalid fs action type: %w", err)
	}
	pwdExpirationThreshold, err := strconv.Atoi(r.Form.Get("pwd_expiration_threshold"))
	if err != nil {
		return dataprovider.BaseEventActionOptions{}, fmt.Errorf("invalid password expiration threshold: %w", err)
	}
	var emailAttachments []string
	if r.Form.Get("email_attachments") != "" {
		emailAttachments = getSliceFromDelimitedValues(r.Form.Get("email_attachments"), ",")
	}
	var cmdArgs []string
	if r.Form.Get("cmd_arguments") != "" {
		cmdArgs = getSliceFromDelimitedValues(r.Form.Get("cmd_arguments"), ",")
	}
	idpMode := 0
	if r.Form.Get("idp_mode") == "1" {
		idpMode = 1
	}
	emailContentType := 0
	if r.Form.Get("email_content_type") == "1" {
		emailContentType = 1
	}
	options := dataprovider.BaseEventActionOptions{
		HTTPConfig: dataprovider.EventActionHTTPConfig{
			Endpoint:        strings.TrimSpace(r.Form.Get("http_endpoint")),
			Username:        strings.TrimSpace(r.Form.Get("http_username")),
			Password:        getSecretFromFormField(r, "http_password"),
			Headers:         getKeyValsFromPostFields(r, "http_header_key", "http_header_val"),
			Timeout:         httpTimeout,
			SkipTLSVerify:   r.Form.Get("http_skip_tls_verify") != "",
			Method:          r.Form.Get("http_method"),
			QueryParameters: getKeyValsFromPostFields(r, "http_query_key", "http_query_val"),
			Body:            r.Form.Get("http_body"),
			Parts:           getHTTPPartsFromPostFields(r),
		},
		CmdConfig: dataprovider.EventActionCommandConfig{
			Cmd:     strings.TrimSpace(r.Form.Get("cmd_path")),
			Args:    cmdArgs,
			Timeout: cmdTimeout,
			EnvVars: getKeyValsFromPostFields(r, "cmd_env_key", "cmd_env_val"),
		},
		EmailConfig: dataprovider.EventActionEmailConfig{
			Recipients:  getSliceFromDelimitedValues(r.Form.Get("email_recipients"), ","),
			Bcc:         getSliceFromDelimitedValues(r.Form.Get("email_bcc"), ","),
			Subject:     r.Form.Get("email_subject"),
			ContentType: emailContentType,
			Body:        r.Form.Get("email_body"),
			Attachments: emailAttachments,
		},
		RetentionConfig: dataprovider.EventActionDataRetentionConfig{
			Folders: foldersRetention,
		},
		FsConfig: dataprovider.EventActionFilesystemConfig{
			Type:    fsActionType,
			Renames: getKeyValsFromPostFields(r, "fs_rename_source", "fs_rename_target"),
			Deletes: getSliceFromDelimitedValues(r.Form.Get("fs_delete_paths"), ","),
			MkDirs:  getSliceFromDelimitedValues(r.Form.Get("fs_mkdir_paths"), ","),
			Exist:   getSliceFromDelimitedValues(r.Form.Get("fs_exist_paths"), ","),
			Copy:    getKeyValsFromPostFields(r, "fs_copy_source", "fs_copy_target"),
			Compress: dataprovider.EventActionFsCompress{
				Name:  strings.TrimSpace(r.Form.Get("fs_compress_name")),
				Paths: getSliceFromDelimitedValues(r.Form.Get("fs_compress_paths"), ","),
			},
		},
		PwdExpirationConfig: dataprovider.EventActionPasswordExpiration{
			Threshold: pwdExpirationThreshold,
		},
		IDPConfig: dataprovider.EventActionIDPAccountCheck{
			Mode:          idpMode,
			TemplateUser:  strings.TrimSpace(r.Form.Get("idp_user")),
			TemplateAdmin: strings.TrimSpace(r.Form.Get("idp_admin")),
		},
	}
	return options, nil
}

func getEventActionFromPostFields(r *http.Request) (dataprovider.BaseEventAction, error) {
	err := r.ParseForm()
	if err != nil {
		return dataprovider.BaseEventAction{}, err
	}
	actionType, err := strconv.Atoi(r.Form.Get("type"))
	if err != nil {
		return dataprovider.BaseEventAction{}, fmt.Errorf("invalid action type: %w", err)
	}
	options, err := getEventActionOptionsFromPostFields(r)
	if err != nil {
		return dataprovider.BaseEventAction{}, err
	}
	action := dataprovider.BaseEventAction{
		Name:        strings.TrimSpace(r.Form.Get("name")),
		Description: r.Form.Get("description"),
		Type:        actionType,
		Options:     options,
	}
	return action, nil
}

func getIDPLoginEventFromPostField(r *http.Request) int {
	switch r.Form.Get("idp_login_event") {
	case "1":
		return 1
	case "2":
		return 2
	default:
		return 0
	}
}

func getEventRuleConditionsFromPostFields(r *http.Request) (dataprovider.EventConditions, error) {
	var schedules []dataprovider.Schedule
	var names, groupNames, roleNames, fsPaths []dataprovider.ConditionPattern
	for k := range r.Form {
		if strings.HasPrefix(k, "schedule_hour") {
			hour := strings.TrimSpace(r.Form.Get(k))
			if hour != "" {
				idx := strings.TrimPrefix(k, "schedule_hour")
				dayOfWeek := strings.TrimSpace(r.Form.Get(fmt.Sprintf("schedule_day_of_week%s", idx)))
				dayOfMonth := strings.TrimSpace(r.Form.Get(fmt.Sprintf("schedule_day_of_month%s", idx)))
				month := strings.TrimSpace(r.Form.Get(fmt.Sprintf("schedule_month%s", idx)))
				schedules = append(schedules, dataprovider.Schedule{
					Hours:      hour,
					DayOfWeek:  dayOfWeek,
					DayOfMonth: dayOfMonth,
					Month:      month,
				})
			}
		}
		if strings.HasPrefix(k, "name_pattern") {
			pattern := strings.TrimSpace(r.Form.Get(k))
			if pattern != "" {
				idx := strings.TrimPrefix(k, "name_pattern")
				patternType := r.Form.Get(fmt.Sprintf("type_name_pattern%s", idx))
				names = append(names, dataprovider.ConditionPattern{
					Pattern:      pattern,
					InverseMatch: patternType == inversePatternType,
				})
			}
		}
		if strings.HasPrefix(k, "group_name_pattern") {
			pattern := strings.TrimSpace(r.Form.Get(k))
			if pattern != "" {
				idx := strings.TrimPrefix(k, "group_name_pattern")
				patternType := r.Form.Get(fmt.Sprintf("type_group_name_pattern%s", idx))
				groupNames = append(groupNames, dataprovider.ConditionPattern{
					Pattern:      pattern,
					InverseMatch: patternType == inversePatternType,
				})
			}
		}
		if strings.HasPrefix(k, "role_name_pattern") {
			pattern := strings.TrimSpace(r.Form.Get(k))
			if pattern != "" {
				idx := strings.TrimPrefix(k, "role_name_pattern")
				patternType := r.Form.Get(fmt.Sprintf("type_role_name_pattern%s", idx))
				roleNames = append(roleNames, dataprovider.ConditionPattern{
					Pattern:      pattern,
					InverseMatch: patternType == inversePatternType,
				})
			}
		}
		if strings.HasPrefix(k, "fs_path_pattern") {
			pattern := strings.TrimSpace(r.Form.Get(k))
			if pattern != "" {
				idx := strings.TrimPrefix(k, "fs_path_pattern")
				patternType := r.Form.Get(fmt.Sprintf("type_fs_path_pattern%s", idx))
				fsPaths = append(fsPaths, dataprovider.ConditionPattern{
					Pattern:      pattern,
					InverseMatch: patternType == inversePatternType,
				})
			}
		}
	}
	minFileSize, err := util.ParseBytes(r.Form.Get("fs_min_size"))
	if err != nil {
		return dataprovider.EventConditions{}, fmt.Errorf("invalid min file size: %w", err)
	}
	maxFileSize, err := util.ParseBytes(r.Form.Get("fs_max_size"))
	if err != nil {
		return dataprovider.EventConditions{}, fmt.Errorf("invalid max file size: %w", err)
	}
	conditions := dataprovider.EventConditions{
		FsEvents:       r.Form["fs_events"],
		ProviderEvents: r.Form["provider_events"],
		IDPLoginEvent:  getIDPLoginEventFromPostField(r),
		Schedules:      schedules,
		Options: dataprovider.ConditionOptions{
			Names:               names,
			GroupNames:          groupNames,
			RoleNames:           roleNames,
			FsPaths:             fsPaths,
			Protocols:           r.Form["fs_protocols"],
			ProviderObjects:     r.Form["provider_objects"],
			MinFileSize:         minFileSize,
			MaxFileSize:         maxFileSize,
			ConcurrentExecution: r.Form.Get("concurrent_execution") != "",
		},
	}
	return conditions, nil
}

func getEventRuleActionsFromPostFields(r *http.Request) ([]dataprovider.EventAction, error) {
	var actions []dataprovider.EventAction
	for k := range r.Form {
		if strings.HasPrefix(k, "action_name") {
			name := strings.TrimSpace(r.Form.Get(k))
			if name != "" {
				idx := strings.TrimPrefix(k, "action_name")
				order, err := strconv.Atoi(r.Form.Get(fmt.Sprintf("action_order%s", idx)))
				if err != nil {
					return actions, fmt.Errorf("invalid order: %w", err)
				}
				options := r.Form[fmt.Sprintf("action_options%s", idx)]
				actions = append(actions, dataprovider.EventAction{
					BaseEventAction: dataprovider.BaseEventAction{
						Name: name,
					},
					Order: order + 1,
					Options: dataprovider.EventActionOptions{
						IsFailureAction: util.Contains(options, "1"),
						StopOnFailure:   util.Contains(options, "2"),
						ExecuteSync:     util.Contains(options, "3"),
					},
				})
			}
		}
	}
	return actions, nil
}

func getEventRuleFromPostFields(r *http.Request) (dataprovider.EventRule, error) {
	err := r.ParseForm()
	if err != nil {
		return dataprovider.EventRule{}, err
	}
	status, err := strconv.Atoi(r.Form.Get("status"))
	if err != nil {
		return dataprovider.EventRule{}, fmt.Errorf("invalid status: %w", err)
	}
	trigger, err := strconv.Atoi(r.Form.Get("trigger"))
	if err != nil {
		return dataprovider.EventRule{}, fmt.Errorf("invalid trigger: %w", err)
	}
	conditions, err := getEventRuleConditionsFromPostFields(r)
	if err != nil {
		return dataprovider.EventRule{}, err
	}
	actions, err := getEventRuleActionsFromPostFields(r)
	if err != nil {
		return dataprovider.EventRule{}, err
	}
	rule := dataprovider.EventRule{
		Name:        strings.TrimSpace(r.Form.Get("name")),
		Status:      status,
		Description: r.Form.Get("description"),
		Trigger:     trigger,
		Conditions:  conditions,
		Actions:     actions,
	}
	return rule, nil
}

func getRoleFromPostFields(r *http.Request) (dataprovider.Role, error) {
	err := r.ParseForm()
	if err != nil {
		return dataprovider.Role{}, err
	}

	return dataprovider.Role{
		Name:        strings.TrimSpace(r.Form.Get("name")),
		Description: r.Form.Get("description"),
	}, nil
}

func getIPListEntryFromPostFields(r *http.Request, listType dataprovider.IPListType) (dataprovider.IPListEntry, error) {
	err := r.ParseForm()
	if err != nil {
		return dataprovider.IPListEntry{}, err
	}
	var mode int
	if listType == dataprovider.IPListTypeDefender {
		mode, err = strconv.Atoi(r.Form.Get("mode"))
		if err != nil {
			return dataprovider.IPListEntry{}, fmt.Errorf("invalid mode: %w", err)
		}
	} else {
		mode = 1
	}
	protocols := 0
	for _, proto := range r.Form["protocols"] {
		p, err := strconv.Atoi(proto)
		if err == nil {
			protocols += p
		}
	}

	return dataprovider.IPListEntry{
		IPOrNet:     strings.TrimSpace(r.Form.Get("ipornet")),
		Mode:        mode,
		Protocols:   protocols,
		Description: r.Form.Get("description"),
	}, nil
}

func getSFTPConfigsFromPostFields(r *http.Request) *dataprovider.SFTPDConfigs {
	return &dataprovider.SFTPDConfigs{
		HostKeyAlgos:  r.Form["sftp_host_key_algos"],
		Moduli:        getSliceFromDelimitedValues(r.Form.Get("sftp_moduli"), ","),
		KexAlgorithms: r.Form["sftp_kex_algos"],
		Ciphers:       r.Form["sftp_ciphers"],
		MACs:          r.Form["sftp_macs"],
	}
}

func getACMEConfigsFromPostFields(r *http.Request) *dataprovider.ACMEConfigs {
	port, err := strconv.Atoi(r.Form.Get("acme_port"))
	if err != nil {
		port = 80
	}
	var protocols int
	for _, val := range r.Form["acme_protocols"] {
		switch val {
		case "1":
			protocols++
		case "2":
			protocols += 2
		case "3":
			protocols += 4
		}
	}

	return &dataprovider.ACMEConfigs{
		Domain:          strings.TrimSpace(r.Form.Get("acme_domain")),
		Email:           strings.TrimSpace(r.Form.Get("acme_email")),
		HTTP01Challenge: dataprovider.ACMEHTTP01Challenge{Port: port},
		Protocols:       protocols,
	}
}

func getSMTPConfigsFromPostFields(r *http.Request) *dataprovider.SMTPConfigs {
	port, err := strconv.Atoi(r.Form.Get("smtp_port"))
	if err != nil {
		port = 587
	}
	authType, err := strconv.Atoi(r.Form.Get("smtp_auth"))
	if err != nil {
		authType = 0
	}
	encryption, err := strconv.Atoi(r.Form.Get("smtp_encryption"))
	if err != nil {
		encryption = 0
	}
	debug := 0
	if r.Form.Get("smtp_debug") != "" {
		debug = 1
	}
	oauth2Provider := 0
	if r.Form.Get("smtp_oauth2_provider") == "1" {
		oauth2Provider = 1
	}
	return &dataprovider.SMTPConfigs{
		Host:       strings.TrimSpace(r.Form.Get("smtp_host")),
		Port:       port,
		From:       strings.TrimSpace(r.Form.Get("smtp_from")),
		User:       strings.TrimSpace(r.Form.Get("smtp_username")),
		Password:   getSecretFromFormField(r, "smtp_password"),
		AuthType:   authType,
		Encryption: encryption,
		Domain:     strings.TrimSpace(r.Form.Get("smtp_domain")),
		Debug:      debug,
		OAuth2: dataprovider.SMTPOAuth2{
			Provider:     oauth2Provider,
			Tenant:       strings.TrimSpace(r.Form.Get("smtp_oauth2_tenant")),
			ClientID:     strings.TrimSpace(r.Form.Get("smtp_oauth2_client_id")),
			ClientSecret: getSecretFromFormField(r, "smtp_oauth2_client_secret"),
			RefreshToken: getSecretFromFormField(r, "smtp_oauth2_refresh_token"),
		},
	}
}

func (s *httpdServer) handleWebAdminForgotPwd(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	if !smtp.IsEnabled() {
		s.renderNotFoundPage(w, r, errors.New("this page does not exist"))
		return
	}
	s.renderForgotPwdPage(w, "", util.GetIPFromRemoteAddress(r.RemoteAddr))
}

func (s *httpdServer) handleWebAdminForgotPwdPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)

	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	err := r.ParseForm()
	if err != nil {
		s.renderForgotPwdPage(w, err.Error(), ipAddr)
		return
	}
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	err = handleForgotPassword(r, r.Form.Get("username"), true)
	if err != nil {
		if e, ok := err.(*util.ValidationError); ok {
			s.renderForgotPwdPage(w, e.GetErrorString(), ipAddr)
			return
		}
		s.renderForgotPwdPage(w, err.Error(), ipAddr)
		return
	}
	http.Redirect(w, r, webAdminResetPwdPath, http.StatusFound)
}

func (s *httpdServer) handleWebAdminPasswordReset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodySize)
	if !smtp.IsEnabled() {
		s.renderNotFoundPage(w, r, errors.New("this page does not exist"))
		return
	}
	s.renderResetPwdPage(w, "", util.GetIPFromRemoteAddress(r.RemoteAddr))
}

func (s *httpdServer) handleWebAdminTwoFactor(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderTwoFactorPage(w, "", util.GetIPFromRemoteAddress(r.RemoteAddr))
}

func (s *httpdServer) handleWebAdminTwoFactorRecovery(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderTwoFactorRecoveryPage(w, "", util.GetIPFromRemoteAddress(r.RemoteAddr))
}

func (s *httpdServer) handleWebAdminMFA(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderMFAPage(w, r)
}

func (s *httpdServer) handleWebAdminProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderProfilePage(w, r, "")
}

func (s *httpdServer) handleWebAdminChangePwd(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderChangePasswordPage(w, r, "")
}

func (s *httpdServer) handleWebAdminProfilePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	err := r.ParseForm()
	if err != nil {
		s.renderProfilePage(w, r, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderProfilePage(w, r, "Invalid token claims")
		return
	}
	admin, err := dataprovider.AdminExists(claims.Username)
	if err != nil {
		s.renderProfilePage(w, r, err.Error())
		return
	}
	admin.Filters.AllowAPIKeyAuth = r.Form.Get("allow_api_key_auth") != ""
	admin.Email = r.Form.Get("email")
	admin.Description = r.Form.Get("description")
	err = dataprovider.UpdateAdmin(&admin, dataprovider.ActionExecutorSelf, ipAddr, admin.Role)
	if err != nil {
		s.renderProfilePage(w, r, err.Error())
		return
	}
	s.renderMessagePage(w, r, "Profile updated", "", http.StatusOK, nil,
		"Your profile has been successfully updated")
}

func (s *httpdServer) handleWebMaintenance(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderMaintenancePage(w, r, "")
}

func (s *httpdServer) handleWebRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRestoreSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	err = r.ParseMultipartForm(MaxRestoreSize)
	if err != nil {
		s.renderMaintenancePage(w, r, err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	restoreMode, err := strconv.Atoi(r.Form.Get("mode"))
	if err != nil {
		s.renderMaintenancePage(w, r, err.Error())
		return
	}
	scanQuota, err := strconv.Atoi(r.Form.Get("quota"))
	if err != nil {
		s.renderMaintenancePage(w, r, err.Error())
		return
	}
	backupFile, _, err := r.FormFile("backup_file")
	if err != nil {
		s.renderMaintenancePage(w, r, err.Error())
		return
	}
	defer backupFile.Close()

	backupContent, err := io.ReadAll(backupFile)
	if err != nil || len(backupContent) == 0 {
		if len(backupContent) == 0 {
			err = errors.New("backup file size must be greater than 0")
		}
		s.renderMaintenancePage(w, r, err.Error())
		return
	}

	if err := restoreBackup(backupContent, "", scanQuota, restoreMode, claims.Username, ipAddr, claims.Role); err != nil {
		s.renderMaintenancePage(w, r, err.Error())
		return
	}

	s.renderMessagePage(w, r, "Data restored", "", http.StatusOK, nil, "Your backup was successfully restored")
}

func (s *httpdServer) handleGetWebAdmins(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	limit := defaultQueryLimit
	if _, ok := r.URL.Query()["qlimit"]; ok {
		var err error
		limit, err = strconv.Atoi(r.URL.Query().Get("qlimit"))
		if err != nil {
			limit = defaultQueryLimit
		}
	}
	admins := make([]dataprovider.Admin, 0, limit)
	for {
		a, err := dataprovider.GetAdmins(limit, len(admins), dataprovider.OrderASC)
		if err != nil {
			s.renderInternalServerErrorPage(w, r, err)
			return
		}
		admins = append(admins, a...)
		if len(a) < limit {
			break
		}
	}
	data := adminsPage{
		basePage: s.getBasePageData(pageAdminsTitle, webAdminsPath, r),
		Admins:   admins,
	}
	renderAdminTemplate(w, templateAdmins, data)
}

func (s *httpdServer) handleWebAdminSetupGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodySize)
	if dataprovider.HasAdmin() {
		http.Redirect(w, r, webAdminLoginPath, http.StatusFound)
		return
	}
	s.renderAdminSetupPage(w, r, "", "")
}

func (s *httpdServer) handleWebAddAdminGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	admin := &dataprovider.Admin{
		Status:      1,
		Permissions: []string{dataprovider.PermAdminAny},
	}
	s.renderAddUpdateAdminPage(w, r, admin, "", true)
}

func (s *httpdServer) handleWebUpdateAdminGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	username := getURLParam(r, "username")
	admin, err := dataprovider.AdminExists(username)
	if err == nil {
		s.renderAddUpdateAdminPage(w, r, &admin, "", false)
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebAddAdminPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	admin, err := getAdminFromPostFields(r)
	if err != nil {
		s.renderAddUpdateAdminPage(w, r, &admin, err.Error(), true)
		return
	}
	if admin.Password == "" && s.binding.isWebAdminLoginFormDisabled() {
		admin.Password = util.GenerateUniqueID()
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	err = dataprovider.AddAdmin(&admin, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderAddUpdateAdminPage(w, r, &admin, err.Error(), true)
		return
	}
	http.Redirect(w, r, webAdminsPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebUpdateAdminPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)

	username := getURLParam(r, "username")
	admin, err := dataprovider.AdminExists(username)
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}

	updatedAdmin, err := getAdminFromPostFields(r)
	if err != nil {
		s.renderAddUpdateAdminPage(w, r, &updatedAdmin, err.Error(), false)
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	updatedAdmin.ID = admin.ID
	updatedAdmin.Username = admin.Username
	if updatedAdmin.Password == "" {
		updatedAdmin.Password = admin.Password
	}
	updatedAdmin.Filters.TOTPConfig = admin.Filters.TOTPConfig
	updatedAdmin.Filters.RecoveryCodes = admin.Filters.RecoveryCodes
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderAddUpdateAdminPage(w, r, &updatedAdmin, "Invalid token claims", false)
		return
	}
	if username == claims.Username {
		if claims.isCriticalPermRemoved(updatedAdmin.Permissions) {
			s.renderAddUpdateAdminPage(w, r, &updatedAdmin, "You cannot remove these permissions to yourself", false)
			return
		}
		if updatedAdmin.Status == 0 {
			s.renderAddUpdateAdminPage(w, r, &updatedAdmin, "You cannot disable yourself", false)
			return
		}
		if updatedAdmin.Role != claims.Role {
			s.renderAddUpdateAdminPage(w, r, &updatedAdmin, "You cannot add/change your role", false)
			return
		}
	}
	err = dataprovider.UpdateAdmin(&updatedAdmin, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderAddUpdateAdminPage(w, r, &updatedAdmin, err.Error(), false)
		return
	}
	http.Redirect(w, r, webAdminsPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebDefenderPage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	data := defenderHostsPage{
		basePage:         s.getBasePageData(pageDefenderTitle, webDefenderPath, r),
		DefenderHostsURL: webDefenderHostsPath,
	}

	renderAdminTemplate(w, templateDefender, data)
}

func (s *httpdServer) handleGetWebUsers(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	var limit int
	if _, ok := r.URL.Query()["qlimit"]; ok {
		var err error
		limit, err = strconv.Atoi(r.URL.Query().Get("qlimit"))
		if err != nil {
			limit = defaultQueryLimit
		}
	} else {
		limit = defaultQueryLimit
	}
	users := make([]dataprovider.User, 0, limit)
	for {
		u, err := dataprovider.GetUsers(limit, len(users), dataprovider.OrderASC, claims.Role)
		if err != nil {
			s.renderInternalServerErrorPage(w, r, err)
			return
		}
		users = append(users, u...)
		if len(u) < limit {
			break
		}
	}
	data := usersPage{
		basePage: s.getBasePageData(pageUsersTitle, webUsersPath, r),
		Users:    users,
	}
	renderAdminTemplate(w, templateUsers, data)
}

func (s *httpdServer) handleWebTemplateFolderGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	if r.URL.Query().Get("from") != "" {
		name := r.URL.Query().Get("from")
		folder, err := dataprovider.GetFolderByName(name)
		if err == nil {
			folder.FsConfig.SetEmptySecrets()
			s.renderFolderPage(w, r, folder, folderPageModeTemplate, "")
		} else if errors.Is(err, util.ErrNotFound) {
			s.renderNotFoundPage(w, r, err)
		} else {
			s.renderInternalServerErrorPage(w, r, err)
		}
	} else {
		folder := vfs.BaseVirtualFolder{}
		s.renderFolderPage(w, r, folder, folderPageModeTemplate, "")
	}
}

func (s *httpdServer) handleWebTemplateFolderPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	templateFolder := vfs.BaseVirtualFolder{}
	err = r.ParseMultipartForm(maxRequestSize)
	if err != nil {
		s.renderMessagePage(w, r, "Error parsing folders fields", "", http.StatusBadRequest, err, "")
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}

	templateFolder.MappedPath = r.Form.Get("mapped_path")
	templateFolder.Description = r.Form.Get("description")
	fsConfig, err := getFsConfigFromPostFields(r)
	if err != nil {
		s.renderMessagePage(w, r, "Error parsing folders fields", "", http.StatusBadRequest, err, "")
		return
	}
	templateFolder.FsConfig = fsConfig

	var dump dataprovider.BackupData
	dump.Version = dataprovider.DumpVersion

	foldersFields := getFoldersForTemplate(r)
	for _, tmpl := range foldersFields {
		f := getFolderFromTemplate(templateFolder, tmpl)
		if err := dataprovider.ValidateFolder(&f); err != nil {
			s.renderMessagePage(w, r, "Folder validation error", fmt.Sprintf("Error validating folder %q", f.Name),
				http.StatusBadRequest, err, "")
			return
		}
		dump.Folders = append(dump.Folders, f)
	}

	if len(dump.Folders) == 0 {
		s.renderMessagePage(w, r, "No folders defined", "No valid folders defined, unable to complete the requested action",
			http.StatusBadRequest, nil, "")
		return
	}
	if r.Form.Get("form_action") == "export_from_template" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"sftpgo-%v-folders-from-template.json\"",
			len(dump.Folders)))
		render.JSON(w, r, dump)
		return
	}
	if err = RestoreFolders(dump.Folders, "", 1, 0, claims.Username, ipAddr, claims.Role); err != nil {
		s.renderMessagePage(w, r, "Unable to save folders", "Cannot save the defined folders:",
			getRespStatus(err), err, "")
		return
	}
	http.Redirect(w, r, webFoldersPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebTemplateUserGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	tokenAdmin := getAdminFromToken(r)
	admin, err := dataprovider.AdminExists(tokenAdmin.Username)
	if err != nil {
		s.renderInternalServerErrorPage(w, r, fmt.Errorf("unable to get the admin %q: %w", tokenAdmin.Username, err))
		return
	}
	if r.URL.Query().Get("from") != "" {
		username := r.URL.Query().Get("from")
		user, err := dataprovider.UserExists(username, admin.Role)
		if err == nil {
			user.SetEmptySecrets()
			user.PublicKeys = nil
			user.Email = ""
			user.Description = ""
			if user.ExpirationDate == 0 && admin.Filters.Preferences.DefaultUsersExpiration > 0 {
				user.ExpirationDate = util.GetTimeAsMsSinceEpoch(time.Now().Add(24 * time.Hour * time.Duration(admin.Filters.Preferences.DefaultUsersExpiration)))
			}
			s.renderUserPage(w, r, &user, userPageModeTemplate, "", &admin)
		} else if errors.Is(err, util.ErrNotFound) {
			s.renderNotFoundPage(w, r, err)
		} else {
			s.renderInternalServerErrorPage(w, r, err)
		}
	} else {
		user := dataprovider.User{BaseUser: sdk.BaseUser{
			Status: 1,
			Permissions: map[string][]string{
				"/": {dataprovider.PermAny},
			},
		}}
		if admin.Filters.Preferences.DefaultUsersExpiration > 0 {
			user.ExpirationDate = util.GetTimeAsMsSinceEpoch(time.Now().Add(24 * time.Hour * time.Duration(admin.Filters.Preferences.DefaultUsersExpiration)))
		}
		s.renderUserPage(w, r, &user, userPageModeTemplate, "", &admin)
	}
}

func (s *httpdServer) handleWebTemplateUserPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	templateUser, err := getUserFromPostFields(r)
	if err != nil {
		s.renderMessagePage(w, r, "Error parsing user fields", "", http.StatusBadRequest, err, "")
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}

	var dump dataprovider.BackupData
	dump.Version = dataprovider.DumpVersion

	userTmplFields := getUsersForTemplate(r)
	for _, tmpl := range userTmplFields {
		u := getUserFromTemplate(templateUser, tmpl)
		if err := dataprovider.ValidateUser(&u); err != nil {
			s.renderMessagePage(w, r, "User validation error", fmt.Sprintf("Error validating user %q", u.Username),
				http.StatusBadRequest, err, "")
			return
		}
		// to create a template the "manage_system" permission is required, so role admins cannot use
		// this method, we don't need to force the role
		dump.Users = append(dump.Users, u)
		for _, folder := range u.VirtualFolders {
			if !dump.HasFolder(folder.Name) {
				dump.Folders = append(dump.Folders, folder.BaseVirtualFolder)
			}
		}
	}

	if len(dump.Users) == 0 {
		s.renderMessagePage(w, r, "No users defined", "No valid users defined, unable to complete the requested action",
			http.StatusBadRequest, nil, "")
		return
	}
	if r.Form.Get("form_action") == "export_from_template" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"sftpgo-%v-users-from-template.json\"",
			len(dump.Users)))
		render.JSON(w, r, dump)
		return
	}
	if err = RestoreUsers(dump.Users, "", 1, 0, claims.Username, ipAddr, claims.Role); err != nil {
		s.renderMessagePage(w, r, "Unable to save users", "Cannot save the defined users:",
			getRespStatus(err), err, "")
		return
	}
	http.Redirect(w, r, webUsersPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebAddUserGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	tokenAdmin := getAdminFromToken(r)
	admin, err := dataprovider.AdminExists(tokenAdmin.Username)
	if err != nil {
		s.renderInternalServerErrorPage(w, r, fmt.Errorf("unable to get the admin %q: %w", tokenAdmin.Username, err))
		return
	}
	user := dataprovider.User{BaseUser: sdk.BaseUser{
		Status: 1,
		Permissions: map[string][]string{
			"/": {dataprovider.PermAny},
		}},
	}
	if admin.Filters.Preferences.DefaultUsersExpiration > 0 {
		user.ExpirationDate = util.GetTimeAsMsSinceEpoch(time.Now().Add(24 * time.Hour * time.Duration(admin.Filters.Preferences.DefaultUsersExpiration)))
	}
	s.renderUserPage(w, r, &user, userPageModeAdd, "", &admin)
}

func (s *httpdServer) handleWebUpdateUserGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	username := getURLParam(r, "username")
	user, err := dataprovider.UserExists(username, claims.Role)
	if err == nil {
		s.renderUserPage(w, r, &user, userPageModeUpdate, "", nil)
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebAddUserPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	user, err := getUserFromPostFields(r)
	if err != nil {
		s.renderUserPage(w, r, &user, userPageModeAdd, err.Error(), nil)
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	log.Println("Request from admin: ", claims.Username)
	admin, err := dataprovider.AdminExists(claims.Username)
	if err != nil {
		log.Println(err)
	}
	for _, item_user_group := range user.Groups {
		x := func(item_user_group string, admin dataprovider.Admin) bool {
			for _, item_admin_group := range admin.Groups {
				if item_user_group == item_admin_group.Name {
					return true
				}
			}
			return false
		}
		if !x(item_user_group.Name, admin) {
			s.renderForbiddenPage(w, r, errors.New("Failed groups. Custom function.").Error())
			return
		}
	}
	for _, item_group := range admin.Groups {
		if item_group.Options.AddToUsersAs == 1 {
			G_TMP, err := dataprovider.GroupExists(item_group.Name)
			if err != nil {
				log.Println(err)
				break
			}
			user.Permissions = G_TMP.UserSettings.Permissions
			NowTime := time.Now().AddDate(0, 0, G_TMP.UserSettings.ExpiresIn).UnixMilli()
			user.HomeDir = G_TMP.UserSettings.HomeDir
			user.ExpirationDate = NowTime
		}
	}

	user = getUserFromTemplate(user, userTemplateFields{
		Username:   user.Username,
		Password:   user.Password,
		PublicKeys: user.PublicKeys,
	})

	if claims.Role != "" {
		user.Role = claims.Role
	}

	user.Filters.RecoveryCodes = nil
	user.Filters.TOTPConfig = dataprovider.UserTOTPConfig{
		Enabled: false,
	}
	err = dataprovider.AddUser(&user, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderUserPage(w, r, &user, userPageModeAdd, err.Error(), nil)
		return
	}
	http.Redirect(w, r, webUsersPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebUpdateUserPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	username := getURLParam(r, "username")
	user, err := dataprovider.UserExists(username, claims.Role)
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	updatedUser, err := getUserFromPostFields(r)
	if err != nil {
		s.renderUserPage(w, r, &user, userPageModeUpdate, err.Error(), nil)
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	updatedUser.ID = user.ID
	updatedUser.Username = user.Username
	updatedUser.Filters.RecoveryCodes = user.Filters.RecoveryCodes
	updatedUser.Filters.TOTPConfig = user.Filters.TOTPConfig
	updatedUser.LastPasswordChange = user.LastPasswordChange
	updatedUser.SetEmptySecretsIfNil()
	if updatedUser.Password == redactedSecret {
		updatedUser.Password = user.Password
	}
	updateEncryptedSecrets(&updatedUser.FsConfig, user.FsConfig.S3Config.AccessSecret, user.FsConfig.AzBlobConfig.AccountKey,
		user.FsConfig.AzBlobConfig.SASURL, user.FsConfig.GCSConfig.Credentials, user.FsConfig.CryptConfig.Passphrase,
		user.FsConfig.SFTPConfig.Password, user.FsConfig.SFTPConfig.PrivateKey, user.FsConfig.SFTPConfig.KeyPassphrase,
		user.FsConfig.HTTPConfig.Password, user.FsConfig.HTTPConfig.APIKey)

	updatedUser = getUserFromTemplate(updatedUser, userTemplateFields{
		Username:   updatedUser.Username,
		Password:   updatedUser.Password,
		PublicKeys: updatedUser.PublicKeys,
	})
	if claims.Role != "" {
		updatedUser.Role = claims.Role
	}

	err = dataprovider.UpdateUser(&updatedUser, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderUserPage(w, r, &updatedUser, userPageModeUpdate, err.Error(), nil)
		return
	}
	if r.Form.Get("disconnect") != "" {
		disconnectUser(user.Username, claims.Username, claims.Role)
	}
	http.Redirect(w, r, webUsersPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebGetStatus(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	data := statusPage{
		basePage: s.getBasePageData(pageStatusTitle, webStatusPath, r),
		Status:   getServicesStatus(),
	}
	renderAdminTemplate(w, templateStatus, data)
}

func (s *httpdServer) handleWebGetConnections(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	connectionStats := common.Connections.GetStats(claims.Role)
	connectionStats = append(connectionStats, getNodesConnections(claims.Username, claims.Role)...)
	data := connectionsPage{
		basePage:    s.getBasePageData(pageConnectionsTitle, webConnectionsPath, r),
		Connections: connectionStats,
	}
	renderAdminTemplate(w, templateConnections, data)
}

func (s *httpdServer) handleWebAddFolderGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderFolderPage(w, r, vfs.BaseVirtualFolder{}, folderPageModeAdd, "")
}

func (s *httpdServer) handleWebAddFolderPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	folder := vfs.BaseVirtualFolder{}
	err = r.ParseMultipartForm(maxRequestSize)
	if err != nil {
		s.renderFolderPage(w, r, folder, folderPageModeAdd, err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	folder.MappedPath = strings.TrimSpace(r.Form.Get("mapped_path"))
	folder.Name = strings.TrimSpace(r.Form.Get("name"))
	folder.Description = r.Form.Get("description")
	fsConfig, err := getFsConfigFromPostFields(r)
	if err != nil {
		s.renderFolderPage(w, r, folder, folderPageModeAdd, err.Error())
		return
	}
	folder.FsConfig = fsConfig
	folder = getFolderFromTemplate(folder, folder.Name)

	err = dataprovider.AddFolder(&folder, claims.Username, ipAddr, claims.Role)
	if err == nil {
		http.Redirect(w, r, webFoldersPath, http.StatusSeeOther)
	} else {
		s.renderFolderPage(w, r, folder, folderPageModeAdd, err.Error())
	}
}

func (s *httpdServer) handleWebUpdateFolderGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	name := getURLParam(r, "name")
	folder, err := dataprovider.GetFolderByName(name)
	if err == nil {
		s.renderFolderPage(w, r, folder, folderPageModeUpdate, "")
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebUpdateFolderPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	name := getURLParam(r, "name")
	folder, err := dataprovider.GetFolderByName(name)
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}

	err = r.ParseMultipartForm(maxRequestSize)
	if err != nil {
		s.renderFolderPage(w, r, folder, folderPageModeUpdate, err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	fsConfig, err := getFsConfigFromPostFields(r)
	if err != nil {
		s.renderFolderPage(w, r, folder, folderPageModeUpdate, err.Error())
		return
	}
	updatedFolder := vfs.BaseVirtualFolder{
		MappedPath:  strings.TrimSpace(r.Form.Get("mapped_path")),
		Description: r.Form.Get("description"),
	}
	updatedFolder.ID = folder.ID
	updatedFolder.Name = folder.Name
	updatedFolder.FsConfig = fsConfig
	updatedFolder.FsConfig.SetEmptySecretsIfNil()
	updateEncryptedSecrets(&updatedFolder.FsConfig, folder.FsConfig.S3Config.AccessSecret, folder.FsConfig.AzBlobConfig.AccountKey,
		folder.FsConfig.AzBlobConfig.SASURL, folder.FsConfig.GCSConfig.Credentials, folder.FsConfig.CryptConfig.Passphrase,
		folder.FsConfig.SFTPConfig.Password, folder.FsConfig.SFTPConfig.PrivateKey, folder.FsConfig.SFTPConfig.KeyPassphrase,
		folder.FsConfig.HTTPConfig.Password, folder.FsConfig.HTTPConfig.APIKey)

	updatedFolder = getFolderFromTemplate(updatedFolder, updatedFolder.Name)

	err = dataprovider.UpdateFolder(&updatedFolder, folder.Users, folder.Groups, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderFolderPage(w, r, updatedFolder, folderPageModeUpdate, err.Error())
		return
	}
	http.Redirect(w, r, webFoldersPath, http.StatusSeeOther)
}

func (s *httpdServer) getWebVirtualFolders(w http.ResponseWriter, r *http.Request, limit int, minimal bool) ([]vfs.BaseVirtualFolder, error) {
	folders := make([]vfs.BaseVirtualFolder, 0, limit)
	for {
		f, err := dataprovider.GetFolders(limit, len(folders), dataprovider.OrderASC, minimal)
		if err != nil {
			s.renderInternalServerErrorPage(w, r, err)
			return folders, err
		}
		folders = append(folders, f...)
		if len(f) < limit {
			break
		}
	}
	return folders, nil
}

func (s *httpdServer) handleWebGetFolders(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	limit := defaultQueryLimit
	if _, ok := r.URL.Query()["qlimit"]; ok {
		var err error
		limit, err = strconv.Atoi(r.URL.Query().Get("qlimit"))
		if err != nil {
			limit = defaultQueryLimit
		}
	}
	folders, err := s.getWebVirtualFolders(w, r, limit, false)
	if err != nil {
		return
	}

	data := foldersPage{
		basePage: s.getBasePageData(pageFoldersTitle, webFoldersPath, r),
		Folders:  folders,
	}
	renderAdminTemplate(w, templateFolders, data)
}

func (s *httpdServer) getWebGroups(w http.ResponseWriter, r *http.Request, limit int, minimal bool) ([]dataprovider.Group, error) {
	groups := make([]dataprovider.Group, 0, limit)
	for {
		f, err := dataprovider.GetGroups(limit, len(groups), dataprovider.OrderASC, minimal)
		if err != nil {
			s.renderInternalServerErrorPage(w, r, err)
			return groups, err
		}
		groups = append(groups, f...)
		if len(f) < limit {
			break
		}
	}
	return groups, nil
}

func (s *httpdServer) handleWebGetGroups(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	limit := defaultQueryLimit
	if _, ok := r.URL.Query()["qlimit"]; ok {
		var err error
		limit, err = strconv.Atoi(r.URL.Query().Get("qlimit"))
		if err != nil {
			limit = defaultQueryLimit
		}
	}
	groups, err := s.getWebGroups(w, r, limit, false)
	if err != nil {
		return
	}

	data := groupsPage{
		basePage: s.getBasePageData(pageGroupsTitle, webGroupsPath, r),
		Groups:   groups,
	}
	renderAdminTemplate(w, templateGroups, data)
}

func (s *httpdServer) handleWebAddGroupGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderGroupPage(w, r, dataprovider.Group{}, genericPageModeAdd, "")
}

func (s *httpdServer) handleWebAddGroupPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	group, err := getGroupFromPostFields(r)
	if err != nil {
		s.renderGroupPage(w, r, group, genericPageModeAdd, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	err = dataprovider.AddGroup(&group, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderGroupPage(w, r, group, genericPageModeAdd, err.Error())
		return
	}
	http.Redirect(w, r, webGroupsPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebUpdateGroupGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	name := getURLParam(r, "name")
	group, err := dataprovider.GroupExists(name)
	if err == nil {
		s.renderGroupPage(w, r, group, genericPageModeUpdate, "")
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebUpdateGroupPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	name := getURLParam(r, "name")
	group, err := dataprovider.GroupExists(name)
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	updatedGroup, err := getGroupFromPostFields(r)
	if err != nil {
		s.renderGroupPage(w, r, group, genericPageModeUpdate, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	updatedGroup.ID = group.ID
	updatedGroup.Name = group.Name
	updatedGroup.SetEmptySecretsIfNil()

	updateEncryptedSecrets(&updatedGroup.UserSettings.FsConfig, group.UserSettings.FsConfig.S3Config.AccessSecret,
		group.UserSettings.FsConfig.AzBlobConfig.AccountKey, group.UserSettings.FsConfig.AzBlobConfig.SASURL,
		group.UserSettings.FsConfig.GCSConfig.Credentials, group.UserSettings.FsConfig.CryptConfig.Passphrase,
		group.UserSettings.FsConfig.SFTPConfig.Password, group.UserSettings.FsConfig.SFTPConfig.PrivateKey,
		group.UserSettings.FsConfig.SFTPConfig.KeyPassphrase, group.UserSettings.FsConfig.HTTPConfig.Password,
		group.UserSettings.FsConfig.HTTPConfig.APIKey)

	err = dataprovider.UpdateGroup(&updatedGroup, group.Users, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderGroupPage(w, r, updatedGroup, genericPageModeUpdate, err.Error())
		return
	}
	http.Redirect(w, r, webGroupsPath, http.StatusSeeOther)
}

func (s *httpdServer) getWebEventActions(w http.ResponseWriter, r *http.Request, limit int, minimal bool,
) ([]dataprovider.BaseEventAction, error) {
	actions := make([]dataprovider.BaseEventAction, 0, limit)
	for {
		res, err := dataprovider.GetEventActions(limit, len(actions), dataprovider.OrderASC, minimal)
		if err != nil {
			s.renderInternalServerErrorPage(w, r, err)
			return actions, err
		}
		actions = append(actions, res...)
		if len(res) < limit {
			break
		}
	}
	return actions, nil
}

func (s *httpdServer) handleWebGetEventActions(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	limit := defaultQueryLimit
	if _, ok := r.URL.Query()["qlimit"]; ok {
		var err error
		limit, err = strconv.Atoi(r.URL.Query().Get("qlimit"))
		if err != nil {
			limit = defaultQueryLimit
		}
	}
	actions, err := s.getWebEventActions(w, r, limit, false)
	if err != nil {
		return
	}

	data := eventActionsPage{
		basePage: s.getBasePageData(pageEventActionsTitle, webAdminEventActionsPath, r),
		Actions:  actions,
	}
	renderAdminTemplate(w, templateEventActions, data)
}

func (s *httpdServer) handleWebAddEventActionGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	action := dataprovider.BaseEventAction{
		Type: dataprovider.ActionTypeHTTP,
	}
	s.renderEventActionPage(w, r, action, genericPageModeAdd, "")
}

func (s *httpdServer) handleWebAddEventActionPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	action, err := getEventActionFromPostFields(r)
	if err != nil {
		s.renderEventActionPage(w, r, action, genericPageModeAdd, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	if err = dataprovider.AddEventAction(&action, claims.Username, ipAddr, claims.Role); err != nil {
		s.renderEventActionPage(w, r, action, genericPageModeAdd, err.Error())
		return
	}
	http.Redirect(w, r, webAdminEventActionsPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebUpdateEventActionGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	name := getURLParam(r, "name")
	action, err := dataprovider.EventActionExists(name)
	if err == nil {
		s.renderEventActionPage(w, r, action, genericPageModeUpdate, "")
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebUpdateEventActionPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	name := getURLParam(r, "name")
	action, err := dataprovider.EventActionExists(name)
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	updatedAction, err := getEventActionFromPostFields(r)
	if err != nil {
		s.renderEventActionPage(w, r, updatedAction, genericPageModeUpdate, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	updatedAction.ID = action.ID
	updatedAction.Name = action.Name
	updatedAction.Options.SetEmptySecretsIfNil()
	switch updatedAction.Type {
	case dataprovider.ActionTypeHTTP:
		if updatedAction.Options.HTTPConfig.Password.IsNotPlainAndNotEmpty() {
			updatedAction.Options.HTTPConfig.Password = action.Options.HTTPConfig.Password
		}
	}
	err = dataprovider.UpdateEventAction(&updatedAction, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderEventActionPage(w, r, updatedAction, genericPageModeUpdate, err.Error())
		return
	}
	http.Redirect(w, r, webAdminEventActionsPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebGetEventRules(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	limit := defaultQueryLimit
	if _, ok := r.URL.Query()["qlimit"]; ok {
		if lim, err := strconv.Atoi(r.URL.Query().Get("qlimit")); err == nil {
			limit = lim
		}
	}
	rules := make([]dataprovider.EventRule, 0, limit)
	for {
		res, err := dataprovider.GetEventRules(limit, len(rules), dataprovider.OrderASC)
		if err != nil {
			s.renderInternalServerErrorPage(w, r, err)
			return
		}
		rules = append(rules, res...)
		if len(res) < limit {
			break
		}
	}

	data := eventRulesPage{
		basePage: s.getBasePageData(pageEventRulesTitle, webAdminEventRulesPath, r),
		Rules:    rules,
	}
	renderAdminTemplate(w, templateEventRules, data)
}

func (s *httpdServer) handleWebAddEventRuleGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	rule := dataprovider.EventRule{
		Status:  1,
		Trigger: dataprovider.EventTriggerFsEvent,
	}
	s.renderEventRulePage(w, r, rule, genericPageModeAdd, "")
}

func (s *httpdServer) handleWebAddEventRulePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	rule, err := getEventRuleFromPostFields(r)
	if err != nil {
		s.renderEventRulePage(w, r, rule, genericPageModeAdd, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	err = verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr)
	if err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	if err = dataprovider.AddEventRule(&rule, claims.Username, ipAddr, claims.Role); err != nil {
		s.renderEventRulePage(w, r, rule, genericPageModeAdd, err.Error())
		return
	}
	http.Redirect(w, r, webAdminEventRulesPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebUpdateEventRuleGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	name := getURLParam(r, "name")
	rule, err := dataprovider.EventRuleExists(name)
	if err == nil {
		s.renderEventRulePage(w, r, rule, genericPageModeUpdate, "")
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebUpdateEventRulePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	name := getURLParam(r, "name")
	rule, err := dataprovider.EventRuleExists(name)
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	updatedRule, err := getEventRuleFromPostFields(r)
	if err != nil {
		s.renderEventRulePage(w, r, updatedRule, genericPageModeUpdate, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	updatedRule.ID = rule.ID
	updatedRule.Name = rule.Name
	err = dataprovider.UpdateEventRule(&updatedRule, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderEventRulePage(w, r, updatedRule, genericPageModeUpdate, err.Error())
		return
	}
	http.Redirect(w, r, webAdminEventRulesPath, http.StatusSeeOther)
}

func (s *httpdServer) getWebRoles(w http.ResponseWriter, r *http.Request, limit int, minimal bool) ([]dataprovider.Role, error) {
	roles := make([]dataprovider.Role, 0, limit)
	for {
		res, err := dataprovider.GetRoles(limit, len(roles), dataprovider.OrderASC, minimal)
		if err != nil {
			s.renderInternalServerErrorPage(w, r, err)
			return roles, err
		}
		roles = append(roles, res...)
		if len(res) < limit {
			break
		}
	}
	return roles, nil
}

func (s *httpdServer) handleWebGetRoles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	roles, err := s.getWebRoles(w, r, 10, false)
	if err != nil {
		return
	}
	data := rolesPage{
		basePage: s.getBasePageData(pageRolesTitle, webAdminRolesPath, r),
		Roles:    roles,
	}
	renderAdminTemplate(w, templateRoles, data)
}

func (s *httpdServer) handleWebAddRoleGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	s.renderRolePage(w, r, dataprovider.Role{}, genericPageModeAdd, "")
}

func (s *httpdServer) handleWebAddRolePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	role, err := getRoleFromPostFields(r)
	if err != nil {
		s.renderRolePage(w, r, role, genericPageModeAdd, err.Error())
		return
	}
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	err = dataprovider.AddRole(&role, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderRolePage(w, r, role, genericPageModeAdd, err.Error())
		return
	}
	http.Redirect(w, r, webAdminRolesPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebUpdateRoleGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	role, err := dataprovider.RoleExists(getURLParam(r, "name"))
	if err == nil {
		s.renderRolePage(w, r, role, genericPageModeUpdate, "")
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebUpdateRolePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	role, err := dataprovider.RoleExists(getURLParam(r, "name"))
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}

	updatedRole, err := getRoleFromPostFields(r)
	if err != nil {
		s.renderRolePage(w, r, role, genericPageModeUpdate, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	updatedRole.ID = role.ID
	updatedRole.Name = role.Name
	err = dataprovider.UpdateRole(&updatedRole, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderRolePage(w, r, updatedRole, genericPageModeUpdate, err.Error())
		return
	}
	http.Redirect(w, r, webAdminRolesPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebGetEvents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)

	data := eventsPage{
		basePage:                s.getBasePageData(pageEventsTitle, webEventsPath, r),
		FsEventsSearchURL:       webEventsFsSearchPath,
		ProviderEventsSearchURL: webEventsProviderSearchPath,
		LogEventsSearchURL:      webEventsLogSearchPath,
	}
	renderAdminTemplate(w, templateEvents, data)
}

func (s *httpdServer) handleWebIPListsPage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	rtlStatus, rtlProtocols := common.Config.GetRateLimitersStatus()
	data := ipListsPage{
		basePage:              s.getBasePageData(pageIPListsTitle, webIPListsPath, r),
		RateLimitersStatus:    rtlStatus,
		RateLimitersProtocols: strings.Join(rtlProtocols, ", "),
		IsAllowListEnabled:    common.Config.IsAllowListEnabled(),
	}

	renderAdminTemplate(w, templateIPLists, data)
}

func (s *httpdServer) handleWebAddIPListEntryGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	listType, _, err := getIPListPathParams(r)
	if err != nil {
		s.renderBadRequestPage(w, r, err)
		return
	}
	s.renderIPListPage(w, r, dataprovider.IPListEntry{Type: listType}, genericPageModeAdd, "")
}

func (s *httpdServer) handleWebAddIPListEntryPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	listType, _, err := getIPListPathParams(r)
	if err != nil {
		s.renderBadRequestPage(w, r, err)
		return
	}
	entry, err := getIPListEntryFromPostFields(r, listType)
	if err != nil {
		s.renderIPListPage(w, r, entry, genericPageModeAdd, err.Error())
		return
	}
	entry.Type = listType
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	err = dataprovider.AddIPListEntry(&entry, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderIPListPage(w, r, entry, genericPageModeAdd, err.Error())
		return
	}
	http.Redirect(w, r, webIPListsPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebUpdateIPListEntryGet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	listType, ipOrNet, err := getIPListPathParams(r)
	if err != nil {
		s.renderBadRequestPage(w, r, err)
		return
	}
	entry, err := dataprovider.IPListEntryExists(ipOrNet, listType)
	if err == nil {
		s.renderIPListPage(w, r, entry, genericPageModeUpdate, "")
	} else if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
	} else {
		s.renderInternalServerErrorPage(w, r, err)
	}
}

func (s *httpdServer) handleWebUpdateIPListEntryPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	listType, ipOrNet, err := getIPListPathParams(r)
	if err != nil {
		s.renderBadRequestPage(w, r, err)
		return
	}
	entry, err := dataprovider.IPListEntryExists(ipOrNet, listType)
	if errors.Is(err, util.ErrNotFound) {
		s.renderNotFoundPage(w, r, err)
		return
	} else if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	updatedEntry, err := getIPListEntryFromPostFields(r, listType)
	if err != nil {
		s.renderIPListPage(w, r, entry, genericPageModeUpdate, err.Error())
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	updatedEntry.Type = listType
	updatedEntry.IPOrNet = ipOrNet
	err = dataprovider.UpdateIPListEntry(&updatedEntry, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderIPListPage(w, r, entry, genericPageModeUpdate, err.Error())
		return
	}
	http.Redirect(w, r, webIPListsPath, http.StatusSeeOther)
}

func (s *httpdServer) handleWebConfigs(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	configs, err := dataprovider.GetConfigs()
	if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	s.renderConfigsPage(w, r, configs, "", 0)
}

func (s *httpdServer) handleWebConfigsPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	claims, err := getTokenClaims(r)
	if err != nil || claims.Username == "" {
		s.renderBadRequestPage(w, r, errors.New("invalid token claims"))
		return
	}
	configs, err := dataprovider.GetConfigs()
	if err != nil {
		s.renderInternalServerErrorPage(w, r, err)
		return
	}
	err = r.ParseForm()
	if err != nil {
		s.renderBadRequestPage(w, r, err)
		return
	}
	ipAddr := util.GetIPFromRemoteAddress(r.RemoteAddr)
	if err := verifyCSRFToken(r.Form.Get(csrfFormToken), ipAddr); err != nil {
		s.renderForbiddenPage(w, r, err.Error())
		return
	}
	var configSection int
	switch r.Form.Get("form_action") {
	case "sftp_submit":
		configSection = 1
		sftpConfigs := getSFTPConfigsFromPostFields(r)
		configs.SFTPD = sftpConfigs
	case "acme_submit":
		configSection = 2
		acmeConfigs := getACMEConfigsFromPostFields(r)
		configs.ACME = acmeConfigs
		if err := acme.GetCertificatesForConfig(acmeConfigs, configurationDir); err != nil {
			s.renderConfigsPage(w, r, configs, err.Error(), configSection)
			return
		}
	case "smtp_submit":
		configSection = 3
		smtpConfigs := getSMTPConfigsFromPostFields(r)
		updateSMTPSecrets(smtpConfigs, configs.SMTP)
		configs.SMTP = smtpConfigs
	default:
		s.renderBadRequestPage(w, r, errors.New("unsupported form action"))
		return
	}

	err = dataprovider.UpdateConfigs(&configs, claims.Username, ipAddr, claims.Role)
	if err != nil {
		s.renderConfigsPage(w, r, configs, err.Error(), configSection)
		return
	}
	if configSection == 3 {
		err := configs.SMTP.TryDecrypt()
		if err == nil {
			smtp.Activate(configs.SMTP)
		} else {
			logger.Error(logSender, "", "unable to decrypt SMTP configuration, cannot activate configuration: %v", err)
		}
	}
	s.renderMessagePage(w, r, "Configurations updated", "", http.StatusOK, nil,
		"Configurations has been successfully updated")
}

func (s *httpdServer) handleOAuth2TokenRedirect(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)

	stateToken := r.URL.Query().Get("state")
	errorTitle := "Unable to complete OAuth2 flow"
	successTitle := "OAuth2 flow completed"

	state, err := verifyOAuth2Token(stateToken, util.GetIPFromRemoteAddress(r.RemoteAddr))
	if err != nil {
		s.renderMessagePage(w, r, errorTitle, "Invalid auth request:", http.StatusBadRequest, err, "")
		return
	}

	defer oauth2Mgr.removePendingAuth(state)

	pendingAuth, err := oauth2Mgr.getPendingAuth(state)
	if err != nil {
		s.renderMessagePage(w, r, errorTitle, "Unable to validate auth request:", http.StatusInternalServerError, err, "")
		return
	}
	oauth2Config := smtp.OAuth2Config{
		Provider:     pendingAuth.Provider,
		ClientID:     pendingAuth.ClientID,
		ClientSecret: pendingAuth.ClientSecret.GetPayload(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := oauth2Config.GetOAuth2()
	cfg.RedirectURL = pendingAuth.RedirectURL
	token, err := cfg.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		s.renderMessagePage(w, r, errorTitle, "Unable to get token:", http.StatusInternalServerError, err, "")
		return
	}
	if token.RefreshToken == "" {
		errTxt := "the OAuth2 provider returned an empty token. " +
			"Some providers only return the token when the user first authorizes. " +
			"If you have already registered SFTPGo with this user in the past, revoke access and try again. " +
			"This way you will invalidate the previous token."
		s.renderMessagePage(w, r, errorTitle, "Unable to get token:", http.StatusBadRequest, errors.New(errTxt), "")
		return
	}
	s.renderMessagePage(w, r, successTitle, "", http.StatusOK, nil,
		fmt.Sprintf("Copy the following string, without the quotes, into SMTP OAuth2 Token configuration field: %q", token.RefreshToken))
}

func updateSMTPSecrets(newConfigs, currentConfigs *dataprovider.SMTPConfigs) {
	if newConfigs.Password.IsNotPlainAndNotEmpty() {
		newConfigs.Password = currentConfigs.Password
	}
	if newConfigs.OAuth2.ClientSecret.IsNotPlainAndNotEmpty() {
		newConfigs.OAuth2.ClientSecret = currentConfigs.OAuth2.ClientSecret
	}
	if newConfigs.OAuth2.RefreshToken.IsNotPlainAndNotEmpty() {
		newConfigs.OAuth2.RefreshToken = currentConfigs.OAuth2.RefreshToken
	}
}
