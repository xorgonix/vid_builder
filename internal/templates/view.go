// Package templates holds server-rendered templ components for vid-build 0.01.
package templates

// ShellData is the shared chrome data for every authenticated page.
type ShellData struct {
	Title            string
	UserName         string
	IsAdmin          bool
	Workspaces       []WorkspaceRef
	CurrentWorkspace *WorkspaceRef
	Project          *EntityRef
	Video            *EntityRef
	ActiveNav        string
	CSRFToken        string
	Flash            string
	FlashKind        string
}

// WorkspaceRef is a minimal workspace reference for navigation.
type WorkspaceRef struct {
	ID   string
	Name string
	Slug string
	Kind string
	Role string
}

// EntityRef is a minimal project/video reference for breadcrumbs.
type EntityRef struct {
	ID    string
	Title string
}

// VideoBase returns the workspace/project/video URL prefix, or "".
func (s ShellData) VideoBase() string {
	if s.CurrentWorkspace == nil || s.Project == nil || s.Video == nil {
		return ""
	}
	return "/w/" + s.CurrentWorkspace.ID + "/projects/" + s.Project.ID + "/videos/" + s.Video.ID
}

// NavItems returns the primary navigation for the current context.
func (s ShellData) NavItems() []NavItem {
	if s.CurrentWorkspace == nil || s.Project == nil {
		return nil
	}
	base := "/w/" + s.CurrentWorkspace.ID + "/projects/" + s.Project.ID
	if s.Video != nil {
		base += "/videos/" + s.Video.ID
	}
	return []NavItem{
		{Key: "develop", Label: "Develop", Href: base + "/develop"},
		{Key: "outline", Label: "Outline", Href: base + "/outline"},
		{Key: "board", Label: "Board", Href: base + "/board"},
		{Key: "produce", Label: "Produce", Href: base + "/produce"},
		{Key: "review", Label: "Review", Href: base + "/review"},
		{Key: "edit", Label: "Edit", Href: base + "/edit"},
		{Key: "more", Label: "More", Href: base + "/more"},
	}
}

// NavItem is one primary navigation entry.
type NavItem struct {
	Key   string
	Label string
	Href  string
}

// LoginData backs the login page.
type LoginData struct {
	CSRFToken string
	Error     string
	Email     string
}

// WorkspaceListData backs the workspace chooser.
type WorkspaceListData struct {
	Shell ShellData
}

// DashboardData backs the workspace dashboard.
type DashboardData struct {
	Shell    ShellData
	Projects []ProjectCard
}

// ProjectCard summarizes a project in lists and dashboards.
type ProjectCard struct {
	ID          string
	Title       string
	Description string
	Status      string
	VideoCount  int
	UpdatedAt   string
}

// ProjectNewData backs the new-project form.
type ProjectNewData struct {
	Shell  ShellData
	Error  string
	Values map[string]string
}

// ProjectView is a project rendered in the UI.
type ProjectView struct {
	ID          string
	Title       string
	Description string
	Status      string
}

// VideoView is a video rendered in the UI.
type VideoView struct {
	ID      string
	Title   string
	Purpose string
	Format  string
	Status  string
	Width   int
	Height  int
}

// ProjectData backs the project dashboard.
type ProjectData struct {
	Shell            ShellData
	Project          ProjectView
	Videos           []VideoView
	Profiles         []ProfileOption
	DefaultProfileID string
}

// VideoData backs a video overview page.
type VideoData struct {
	Shell        ShellData
	Project      ProjectView
	Video        VideoView
	Sections     int
	Scenes       int
	Shots        int
	BriefCurrent bool
}

// BriefView is an accepted brief rendered in the UI.
type BriefView struct {
	Version         int
	Premise         string
	Theme           string
	Audience        string
	DesiredReaction string
	Pace            string
	Excitement      string
	MusicIntent     string
	SoundIntent     string
	VisualStyle     string
	CameraLanguage  string
	ProductionNotes string
}

// MessageView is one discovery message.
type MessageView struct {
	Role      string
	Content   string
	ModelID   string
	CreatedAt string
}

// ThreadView is a discovery thread summary.
type ThreadView struct {
	ID           string
	Title        string
	MessageCount int
}

// ProposalView is a proposal summary.
type ProposalView struct {
	ID        string
	Type      string
	Summary   string
	Status    string
	CreatedAt string
}

// DevelopData backs the Develop screen.
type DevelopData struct {
	Shell          ShellData
	Project        ProjectView
	Video          VideoView
	Brief          *BriefView
	Threads        []ThreadView
	ActiveThread   string
	Messages       []MessageView
	Proposals      []ProposalView
	PlannerEnabled bool
	Error          string
}

// FieldView is one selectable proposal field for the apply form.
type FieldView struct {
	Key     string
	Label   string
	Value   string
	Checked bool
}

// ProposalData backs the proposal diff/apply screen.
type ProposalData struct {
	Shell       ShellData
	Project     ProjectView
	Video       VideoView
	Proposal    ProposalView
	Fields      []FieldView
	Outline     *OutlineView
	Blocking    *BlockingPreview
	IsOutline   bool
	IsBrief     bool
	IsPrompt    bool
	IsBlocking  bool
	OutlineJSON string
}

// BlockingPreview previews a proposed start/end blocking.
type BlockingPreview struct {
	ShotID   string
	Summary  string
	StartSVG string
	EndSVG   string
}

// OutlineView is a proposed outline payload.
type OutlineView struct {
	Summary  string
	Sections []SectionNode
}

// BriefHistoryData backs the brief history drawer/page.
type BriefHistoryData struct {
	Shell    ShellData
	Project  ProjectView
	Video    VideoView
	Versions []BriefView
}

// SpecView is one shot specification in the inspector.
type SpecView struct {
	Kind       string
	Label      string
	Positive   string
	Negative   string
	Structured string
	Version    int
}

// BlockingView is a rendered blocking for an endpoint.
type BlockingView struct {
	Role     string
	SpecJSON string
	Source   string
	Version  int
	Exists   bool
}

// ShotDetailData backs the shot inspector.
type ShotDetailData struct {
	Shell      ShellData
	Project    ProjectView
	Video      VideoView
	Shot       ShotNode
	Specs      []SpecView
	StartBlock BlockingView
	EndBlock   BlockingView
	History    []HistoryItem
}

// HistoryItem is a line in the shot history.
type HistoryItem struct {
	When   string
	Action string
	Detail string
}

// BoardCard is a storyboard card.
type BoardCard struct {
	ShotID     string
	Title      string
	Purpose    string
	SceneTitle string
	Readiness  string
	Status     string
	DurationMs int
	StartSVG   string
	EndSVG     string
	HasStart   bool
	HasEnd     bool
	MotionText string
	Stale      bool
}

// BoardData backs the storyboard.
type BoardData struct {
	Shell   ShellData
	Project ProjectView
	Video   VideoView
	Cards   []BoardCard
	Mode    string
	Filter  string
	Total   int
}

// BlockingEditorData backs the paired blocking editor.
type BlockingEditorData struct {
	Shell     ShellData
	Project   ProjectView
	Video     VideoView
	Shot      ShotNode
	Start     BlockingView
	End       BlockingView
	StartSVG  string
	EndSVG    string
	Analysis  []string
	SpecStart SpecView
	SpecEnd   SpecView
}

// BibleData backs the project bible.
type BibleData struct {
	Shell      ShellData
	Project    ProjectView
	Characters []CharacterView
	Locations  []LocationView
	StyleRules []StyleRuleView
}

// CharacterView is a character in the bible.
type CharacterView struct {
	ID          string
	Name        string
	Description string
	Continuity  string
}

// LocationView is a location in the bible.
type LocationView struct {
	ID          string
	Name        string
	Description string
	Continuity  string
}

// StyleRuleView is a style rule in the bible.
type StyleRuleView struct {
	ID       string
	Category string
	Text     string
	Priority int
}

// MoreData backs the More menu.
type MoreData struct {
	Shell   ShellData
	Project ProjectView
	Video   VideoView
}

// ReadyShot is a shot that can be sent to production.
type ReadyShot struct {
	ShotID      string
	Title       string
	Purpose     string
	SceneTitle  string
	Readiness   string
	StartReady  bool
	EndReady    bool
	MotionReady bool
}

// RequestView is a generation request row.
type RequestView struct {
	ID         string
	ShotID     string
	ShotTitle  string
	Role       string
	Tier       string
	Candidates int
	Status     string
	CreatedAt  string
	JobID      string
}

// JobView is a job row.
type JobView struct {
	ID          string
	Type        string
	State       string
	AggregateID string
	CreatedAt   string
}

// ProduceData backs the Produce screen.
type ProduceData struct {
	Shell    ShellData
	Project  ProjectView
	Video    VideoView
	Ready    []ReadyShot
	Requests []RequestView
	Jobs     []JobView
}

// GenerateData backs the generation request form.
type GenerateData struct {
	Shell                     ShellData
	Project                   ProjectView
	Video                     VideoView
	Shot                      ShotNode
	Role                      string
	RoleLabel                 string
	Positive                  string
	Negative                  string
	Width                     int
	Height                    int
	WorkflowID                string
	WorkflowName              string
	Workflows                 []WorkflowOption
	Checkpoints               []string
	DefaultModel              string
	ComfyReady                bool
	ComfyError                string
	SeedPolicy                string
	RequirementsMet           bool
	IsMotion                  bool
	EndpointsReady            bool
	EndpointNote              string
	WorkflowSupportsEndpoints bool
	Profiles                  []ProfileOption
	DefaultProfileID          string
}

// JobEventView is a job event line.
type JobEventView struct {
	Sequence  int
	Phase     string
	Level     string
	Message   string
	Progress  int
	CreatedAt string
}

// AttemptView is an attempt row.
type AttemptView struct {
	Number     int
	Status     string
	Seed       string
	Model      string
	DurationMs string
	Error      string
	Endpoints  string
}

// AssetView is an asset rendered in the UI.
type AssetView struct {
	ID        string
	Kind      string
	Role      string
	MimeType  string
	URL       string
	Checksum  string
	SizeBytes int64
	CreatedAt string
	Seed      string
	Model     string
	Duration  string
}

// JobPageData backs the job detail page.
type JobPageData struct {
	Shell    ShellData
	Project  ProjectView
	Video    VideoView
	Job      JobView
	Events   []JobEventView
	Attempts []AttemptView
	Assets   []AssetView
}

// ReviewGroup groups candidate assets for a shot/role.
type ReviewGroup struct {
	ShotID     string
	ShotTitle  string
	Role       string
	RoleLabel  string
	Assets     []AssetView
	SelectedID string
	Decision   string
	Stale      bool
}

// ReviewData backs the Review screen.
type ReviewData struct {
	Shell       ShellData
	Project     ProjectView
	Video       VideoView
	Groups      []ReviewGroup
	Annotations []AnnotationView
}

// AnnotationView is a review note.
type AnnotationView struct {
	ID        string
	Body      string
	Status    string
	Author    string
	CreatedAt string
	ShotID    string
	ShotTitle string
	AssetID   string
}

// WorkflowView is a workflow package row.
type WorkflowView struct {
	PackageID     string
	Name          string
	Slug          string
	Task          string
	Source        string
	VersionID     string
	VersionStatus string
	Bindings      int
}

// WorkflowsData backs the workflow list.
type WorkflowsData struct {
	Shell     ShellData
	Workflows []WorkflowView
}

// NodeClassCount is a detected ComfyUI node class.
type NodeClassCount struct {
	Class string
	Count int
}

// BindingRow is an editable semantic binding.
type BindingRow struct {
	Field     string
	NodeID    string
	InputName string
	Required  bool
}

// WorkflowVersionData backs the workflow binding/validation screen.
type WorkflowVersionData struct {
	Shell           ShellData
	PackageID       string
	PackageName     string
	Source          string
	VersionID       string
	Status          string
	GraphSHA        string
	SemanticVersion string
	NodeCount       int
	Classes         []NodeClassCount
	Bindings        []BindingRow
	BoundFields     []string
	AllFields       []string
}

// WorkflowOption is a selectable workflow version.
type WorkflowOption struct {
	ID      string
	Name    string
	Version string
	Status  string
}

// ProfileView is a model profile row.
type ProfileView struct {
	ID         string
	Name       string
	Task       string
	Checkpoint string
}

// ProfileOption is a selectable model profile.
type ProfileOption struct {
	ID         string
	Name       string
	Checkpoint string
}

// ModelProfilesData backs the model profiles screen.
type ModelProfilesData struct {
	Shell       ShellData
	Profiles    []ProfileView
	Checkpoints []string
	ComfyReady  bool
}

// MemberView is a workspace member row.
type MemberView struct {
	UserID      string
	Email       string
	DisplayName string
	Role        string
	Status      string
	JoinedAt    string
	IsSelf      bool
}

// MembersData backs the membership screen.
type MembersData struct {
	Shell   ShellData
	Members []MemberView
	CanEdit bool
}

// AdminUserView is a user row in administration.
type AdminUserView struct {
	ID          string
	Email       string
	DisplayName string
	Status      string
	IsAdmin     bool
	IsSelf      bool
	CreatedAt   string
}

// AdminUsersData backs the user-administration screen.
type AdminUsersData struct {
	Shell ShellData
	Users []AdminUserView
	Error string
}

// PackageCount is one row in the package preview.
type PackageCount struct {
	Label string
	Value int
}

// PackageData backs the project package preview.
type PackageData struct {
	Shell      ShellData
	Project    ProjectView
	Counts     []PackageCount
	MediaCount int
	Latest     *AssetView
}

// LayerView is one editable prompt layer.
type LayerView struct {
	Scope  string
	Mode   string
	Text   string
	Locked bool
}

// PromptWorkbenchData backs the layered prompt workbench.
type PromptWorkbenchData struct {
	Shell            ShellData
	Project          ProjectView
	Video            VideoView
	Shot             ShotNode
	Kind             string
	KindLabel        string
	BasePositive     string
	BaseNegative     string
	Layers           []LayerView
	Scopes           []string
	Modes            []string
	Positive         string
	Negative         string
	CompiledPositive string
	CompiledNegative string
	Applied          []string
}

// EditItemView is one clip in an edit.
type EditItemView struct {
	ID            string
	ShotID        string
	ShotTitle     string
	AssetID       string
	AssetURL      string
	Kind          string
	Position      int
	DurationMs    int
	TransitionIn  string
	TransitionOut string
	Revision      int
}

// SelectionOption is a selected asset that can be added to the edit.
type SelectionOption struct {
	ShotID    string
	ShotTitle string
	Role      string
	RoleLabel string
	AssetID   string
	AssetURL  string
}

// EditData backs the Edit screen.
type EditData struct {
	Shell    ShellData
	Project  ProjectView
	Video    VideoView
	Edit     EditView
	Items    []EditItemView
	Options  []SelectionOption
	TotalMs  int
	Warnings []string
	Exports  []ExportView
}

// EditView is an edit header.
type EditView struct {
	ID       string
	Name     string
	Status   string
	Revision int
}

// ExportView is an export row.
type ExportView struct {
	ID        string
	Status    string
	CreatedAt string
	AssetID   string
	Error     string
}

// ExportData backs the export receipt page.
type ExportData struct {
	Shell  ShellData
	Export ExportView
	JobID  string
	Asset  *AssetView
}

// TargetEstimate compares an execution target's measured performance.
type TargetEstimate struct {
	ID          string
	Name        string
	Kind        string
	Enabled     bool
	AvgMs       int64
	Samples     int
	EstimatedMs int64
	Currency    string
	Note        string
}

// CampaignView is a campaign row.
type CampaignView struct {
	ID          string
	Name        string
	Status      string
	TargetName  string
	Items       int
	EstimatedMs int64
	ActualMs    int64
	CreatedAt   string
}

// ComputeData backs the compute planner.
type ComputeData struct {
	Shell     ShellData
	Project   ProjectView
	Video     VideoView
	Drafts    []RequestView
	Targets   []TargetEstimate
	Campaigns []CampaignView
}

// CampaignItemView is one scheduled request.
type CampaignItemView struct {
	RequestID   string
	ShotTitle   string
	Role        string
	Candidates  int
	Status      string
	EstimatedMs int64
}

// CampaignPageData backs a campaign detail page.
type CampaignPageData struct {
	Shell     ShellData
	Project   ProjectView
	Video     VideoView
	Campaign  CampaignView
	Items     []CampaignItemView
	Target    TargetEstimate
	CanLaunch bool
}

// LocalServicesData backs the local-services settings page.
type LocalServicesData struct {
	Shell       ShellData
	LLMModel    string
	LLMBase     string
	ComfyBase   string
	FFmpegBin   string
	LLMEnabled  bool
	ComfyEnable bool
}

// DiagnosticsData backs the diagnostics page.
type DiagnosticsData struct {
	Shell              ShellData
	JobsQueued         int
	JobsRunning        int
	JobsFailed         int
	ObservationsFailed int
	AssetCount         int
	AttemptCount       int
	DBIntegrity        string
	FKViolations       int
	JournalMode        string
	MediaRootOK        bool
	WorkingDir         string
	Platform           string
}

// OutlineData backs the Outline screen.
type OutlineData struct {
	Shell    ShellData
	Project  ProjectView
	Video    VideoView
	Sections []SectionNode
}

// SectionNode is a section with its scenes.
type SectionNode struct {
	ID       string
	Title    string
	Purpose  string
	Position int
	Revision int
	BudgetMs int
	Scenes   []SceneNode
}

// SceneNode is a scene with its shots.
type SceneNode struct {
	ID       string
	Title    string
	Synopsis string
	Purpose  string
	Pace     string
	Reaction string
	Position int
	Revision int
	BudgetMs int
	Shots    []ShotNode
}

// ShotNode is a shot row in the outline.
type ShotNode struct {
	ID         string
	Title      string
	Purpose    string
	Treatment  string
	Position   int
	Revision   int
	DurationMs int
	Status     string
	Readiness  string
	SpecsReady int
}
