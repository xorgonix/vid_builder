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
