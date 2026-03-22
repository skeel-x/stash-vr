package playa

type Configuration struct {
	SiteName         string  `json:"site_name"`
	SiteLogo         *string `json:"site_logo,omitempty"`
	Auth             bool    `json:"auth"`
	AuthByCode       bool    `json:"auth_by_code"`
	Actors           bool    `json:"actors"`
	Categories       bool    `json:"categories"`
	CategoriesGroups bool    `json:"categories_groups"`
	Studios          bool    `json:"studios"`
	Scripts          bool    `json:"scripts"`
	Analytics        bool    `json:"analytics"`
	Masks            bool    `json:"masks"`
}

type Page[T any] struct {
	PageIndex int `json:"page_index"`
	PageSize  int `json:"page_size"`
	PageTotal int `json:"page_total"`
	ItemTotal int `json:"item_total"`
	Content   []T `json:"content"`
}

type CategoryListView struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Preview *string `json:"preview,omitempty"`
}

type CategoriesGroup struct {
	ID    string             `json:"id"`
	Title string             `json:"title"`
	Items []CategoryListView `json:"items"`
}

type VideoStatus struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type VideoListView struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Subtitle     *string           `json:"subtitle,omitempty"`
	Status       string            `json:"status,omitempty"`
	PreviewImage *string           `json:"preview_image,omitempty"`
	HasScripts   bool              `json:"has_scripts"`
	ReleaseDate  *int64            `json:"release_date,omitempty"`
	Details      []VideoListDetail `json:"details,omitempty"`
}

type VideoListDetail struct {
	Type            string `json:"type"`
	DurationSeconds *int   `json:"duration_seconds,omitempty"`
	HasScripts      bool   `json:"has_scripts"`
}

type VideoView struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Subtitle     *string       `json:"subtitle,omitempty"`
	Status       string        `json:"status,omitempty"`
	Description  *string       `json:"description,omitempty"`
	PreviewImage *string       `json:"preview_image,omitempty"`
	ReleaseDate  *int64        `json:"release_date,omitempty"`
	Studio       *StudioRef    `json:"studio,omitempty"`
	Categories   []CategoryRef `json:"categories,omitempty"`
	Actors       []ActorRef    `json:"actors,omitempty"`
	Views        *int          `json:"views,omitempty"`
	Details      []VideoDetail `json:"details,omitempty"`
}

type StudioRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type CategoryRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ActorRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type VideoDetail struct {
	Type            string           `json:"type"`
	DurationSeconds *int             `json:"duration_seconds,omitempty"`
	TimelineMarkers []TimelineMarker `json:"timeline_markers,omitempty"`
	Links           []VideoLinkView  `json:"links,omitempty"`
	ScriptInfo      *VideoScriptInfo `json:"script_info,omitempty"`
}

type TimelineMarker struct {
	Time  int64   `json:"time"`
	Title *string `json:"title,omitempty"`
}

type VideoLinkView struct {
	IsStream          bool    `json:"is_stream"`
	IsDownload        bool    `json:"is_download"`
	URL               *string `json:"url,omitempty"`
	UnavailableReason *string `json:"unavailable_reason,omitempty"`
	Projection        string  `json:"projection"`
	Stereo            string  `json:"stereo"`
	QualityName       string  `json:"quality_name"`
	QualityOrder      int     `json:"quality_order"`
}

type VideoScriptInfo struct {
	ID               string `json:"id"`
	GenerationSource int    `json:"generation_source"`
}

type ActorListView struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Preview *string `json:"preview,omitempty"`
}

type ActorView struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Preview    *string         `json:"preview,omitempty"`
	Studios    []StudioRef     `json:"studios,omitempty"`
	Properties []ActorProperty `json:"properties,omitempty"`
	Aliases    []string        `json:"aliases,omitempty"`
	Views      *int            `json:"views,omitempty"`
	Banner     *string         `json:"banner,omitempty"`
}

type ActorProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type StudioListView struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Preview *string `json:"preview,omitempty"`
}

type StudioView struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Preview     *string `json:"preview,omitempty"`
	Description *string `json:"description,omitempty"`
	Views       *int    `json:"views,omitempty"`
}
