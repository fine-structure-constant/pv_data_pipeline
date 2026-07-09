package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Paper represents a discovered scholarly work and its lifecycle metadata in the pipeline.
type Paper struct {
	ID               string               `gorm:"type:uuid;primaryKey" json:"id"`
	DOI              string               `gorm:"uniqueIndex;size:512" json:"doi"`
	Title            string               `gorm:"index;type:text" json:"title"`
	TitleHash        string               `gorm:"index;size:64" json:"title_hash"`
	Abstract         string               `gorm:"type:text" json:"abstract"`
	Journal          string               `gorm:"index" json:"journal"`
	Year             int                  `gorm:"index" json:"year"`
	PublishedDate    *time.Time           `json:"published_date,omitempty"`
	Authors          datatypes.JSON       `gorm:"type:jsonb" json:"authors"`
	URL              string               `gorm:"type:text" json:"url"`
	SourceAPI        string               `gorm:"index" json:"source_api"`
	RawMetadata      datatypes.JSON       `gorm:"type:jsonb" json:"raw_metadata"`
	CrawlStatus      string               `gorm:"index;default:pending" json:"crawl_status"`
	DownloadStatus   string               `gorm:"index;default:metadata_only" json:"download_status"`
	ExtractionStatus string               `gorm:"index;default:not_started" json:"extraction_status"`
	LocalDir         string               `gorm:"type:text" json:"local_dir"`
	Assets           []PaperAsset         `json:"assets,omitempty"`
	Classes          []PaperMaterialClass `json:"classes,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

// BeforeCreate ensures a paper gets a UUID before being persisted.
func (p *Paper) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	return nil
}

// PaperAsset stores the downloaded or discovered artifact associated with a paper.
type PaperAsset struct {
	ID           string     `gorm:"type:uuid;primaryKey" json:"id"`
	PaperID      string     `gorm:"type:uuid;index;not null" json:"paper_id"`
	Paper        Paper      `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	AssetType    string     `gorm:"index" json:"asset_type"`
	SourceURL    string     `gorm:"type:text" json:"source_url"`
	LocalPath    string     `gorm:"type:text" json:"local_path"`
	SHA256       string     `gorm:"size:64" json:"sha256"`
	MIMEType     string     `json:"mime_type"`
	License      string     `json:"license"`
	AccessType   string     `gorm:"index" json:"access_type"`
	DownloadedAt *time.Time `json:"downloaded_at,omitempty"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// BeforeCreate ensures a paper asset receives a UUID before insertion.
func (a *PaperAsset) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	return nil
}

// MaterialClass groups papers into high-level material family labels.
type MaterialClass struct {
	ID          string    `gorm:"type:uuid;primaryKey" json:"id"`
	Code        string    `gorm:"uniqueIndex;not null" json:"code"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BeforeCreate ensures a material class gets a UUID when created.
func (m *MaterialClass) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return nil
}

// PaperMaterialClass records the link between a paper and a material-class assignment.
type PaperMaterialClass struct {
	ID              string        `gorm:"type:uuid;primaryKey" json:"id"`
	PaperID         string        `gorm:"type:uuid;uniqueIndex:idx_paper_class_source;not null" json:"paper_id"`
	Paper           Paper         `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	MaterialClassID string        `gorm:"type:uuid;uniqueIndex:idx_paper_class_source;not null" json:"material_class_id"`
	MaterialClass   MaterialClass `json:"material_class"`
	Confidence      float64       `json:"confidence"`
	AssignedBy      string        `gorm:"uniqueIndex:idx_paper_class_source;size:32" json:"assigned_by"`
	EvidenceText    string        `gorm:"type:text" json:"evidence_text"`
	CreatedAt       time.Time     `json:"created_at"`
}

// BeforeCreate ensures a paper-class assignment receives an identifier before insert.
func (p *PaperMaterialClass) BeforeCreate(tx *gorm.DB) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	return nil
}

// LLMClassification stores the raw and parsed output of an LLM classification request.
type LLMClassification struct {
	ID            string         `gorm:"type:uuid;primaryKey" json:"id"`
	PaperID       string         `gorm:"type:uuid;index;not null" json:"paper_id"`
	Paper         Paper          `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	ModelName     string         `json:"model_name"`
	PromptVersion string         `gorm:"index" json:"prompt_version"`
	InputTextHash string         `gorm:"index;size:64" json:"input_text_hash"`
	RawResponse   string         `gorm:"type:text" json:"raw_response"`
	ParsedJSON    datatypes.JSON `gorm:"type:jsonb" json:"parsed_json"`
	IsRelevant    bool           `gorm:"index" json:"is_relevant"`
	IsMAPbI3Only  bool           `gorm:"index" json:"is_mapbi3_only"`
	Confidence    float64        `json:"confidence"`
	ErrorMessage  string         `gorm:"type:text" json:"error_message"`
	CreatedAt     time.Time      `json:"created_at"`
}

// BeforeCreate ensures an LLM classification record gets a UUID before persistence.
func (l *LLMClassification) BeforeCreate(tx *gorm.DB) error {
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	return nil
}

// CrawlJob tracks a batch crawl operation and its aggregate statistics.
type CrawlJob struct {
	ID           string     `gorm:"type:uuid;primaryKey" json:"id"`
	Query        string     `gorm:"type:text" json:"query"`
	Source       string     `gorm:"index" json:"source"`
	Status       string     `gorm:"index" json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	NumFound     int        `json:"num_found"`
	NumInserted  int        `json:"num_inserted"`
	NumUpdated   int        `json:"num_updated"`
	NumFailed    int        `json:"num_failed"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	Logs         []CrawlLog `json:"logs,omitempty"`
}

// BeforeCreate ensures a crawl job gets a UUID before being recorded.
func (c *CrawlJob) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	return nil
}

// CrawlLog stores individual log entries emitted during a crawl job.
type CrawlLog struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	CrawlJobID string    `gorm:"type:uuid;index" json:"crawl_job_id"`
	Level      string    `json:"level"`
	Message    string    `gorm:"type:text" json:"message"`
	DOI        string    `gorm:"index" json:"doi"`
	CreatedAt  time.Time `json:"created_at"`
}

// BeforeCreate ensures a crawl log entry acquires a UUID before insertion.
func (c *CrawlLog) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	return nil
}

// Material represents a chemical or device material concept linked to papers.
type Material struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `gorm:"index" json:"name"`
	Notes     string    `gorm:"type:text" json:"notes"`
	Papers    []Paper   `gorm:"many2many:paper_materials" json:"papers,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeCreate ensures a material record gets a UUID before persistence.
func (m *Material) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return nil
}

// Composition captures the formula and normalized composition of a material entry.
type Composition struct {
	ID          string         `gorm:"type:uuid;primaryKey" json:"id"`
	MaterialID  string         `gorm:"type:uuid;index" json:"material_id"`
	FormulaRaw  string         `gorm:"type:text" json:"formula_raw"`
	Normalized  string         `gorm:"index" json:"normalized"`
	Composition datatypes.JSON `gorm:"type:jsonb" json:"composition"`
	CreatedAt   time.Time      `json:"created_at"`
}

// BeforeCreate ensures a composition record gets a UUID before insertion.
func (c *Composition) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	return nil
}

// Structure describes a structural phase associated with a material.
type Structure struct {
	ID         string         `gorm:"type:uuid;primaryKey" json:"id"`
	MaterialID string         `gorm:"type:uuid;index" json:"material_id"`
	Phase      string         `json:"phase"`
	Metadata   datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	CreatedAt  time.Time      `json:"created_at"`
}

// BeforeCreate ensures a structure record receives a UUID before persistence.
func (s *Structure) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	return nil
}

// Device represents a device stack connected to a paper and material.
type Device struct {
	ID         string         `gorm:"type:uuid;primaryKey" json:"id"`
	MaterialID string         `gorm:"type:uuid;index" json:"material_id"`
	PaperID    string         `gorm:"type:uuid;index" json:"paper_id"`
	Stack      string         `gorm:"type:text" json:"stack"`
	Metadata   datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	CreatedAt  time.Time      `json:"created_at"`
}

// BeforeCreate ensures a device record gets a UUID before insertion.
func (d *Device) BeforeCreate(tx *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	return nil
}

// Measurement stores performance metrics captured for a device in a paper.
type Measurement struct {
	ID        string         `gorm:"type:uuid;primaryKey" json:"id"`
	DeviceID  string         `gorm:"type:uuid;index" json:"device_id"`
	PaperID   string         `gorm:"type:uuid;index" json:"paper_id"`
	PCE       *float64       `json:"pce,omitempty"`
	Voc       *float64       `json:"voc,omitempty"`
	Jsc       *float64       `json:"jsc,omitempty"`
	FF        *float64       `json:"ff,omitempty"`
	Bandgap   *float64       `json:"bandgap,omitempty"`
	Metadata  datatypes.JSON `gorm:"type:jsonb" json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

// BeforeCreate ensures a measurement record gets a UUID before persistence.
func (m *Measurement) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	return nil
}

var DefaultMaterialClasses = []MaterialClass{
	{Code: "FA_PB_I3", Description: "Formamidinium lead iodide family"},
	{Code: "CS_PB_I2_BR", Description: "Cesium lead iodide bromide family"},
	{Code: "FA_MA_PB_I_BR", Description: "FA/MA mixed-cation iodide/bromide"},
	{Code: "FA_SN_I3", Description: "Formamidinium tin iodide family"},
	{Code: "FA_RICH", Description: "FA-rich perovskite"},
	{Code: "CS_CONTAINING", Description: "Cs-containing perovskite"},
	{Code: "SN_BASED", Description: "Tin-based or tin-containing perovskite"},
	{Code: "PB_BASED", Description: "Lead-based perovskite"},
	{Code: "MIXED_CATION", Description: "Mixed A-site cation perovskite"},
	{Code: "MIXED_HALIDE", Description: "Mixed-halide perovskite"},
	{Code: "I_BR_MIXED", Description: "Iodide/bromide mixed halide"},
	{Code: "WIDE_BANDGAP", Description: "Wide-bandgap perovskite"},
	{Code: "LOW_DIMENSIONAL", Description: "Low-dimensional perovskite"},
	{Code: "THREE_D", Description: "3D perovskite"},
	{Code: "NOT_MA_PB_I3", Description: "Not a pure MAPbI3 baseline"},
	{Code: "MAPBI3_BASELINE", Description: "Pure MAPbI3 baseline or early benchmark"},
	{Code: "IRRELEVANT", Description: "Not relevant to halide perovskite solar cells"},
}
