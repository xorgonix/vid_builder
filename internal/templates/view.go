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
	Shell   ShellData
	Project ProjectView
	Videos  []VideoView
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
