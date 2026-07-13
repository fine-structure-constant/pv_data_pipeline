package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"pvsk-pipeline/internal/models"

	"gorm.io/gorm"
)

type Server struct {
	DB *gorm.DB
}

type tableDef struct {
	Name        string
	Label       string
	Model       any
	SearchCols  []string
	DefaultSort string
}

type tableSummary struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

type tableResult struct {
	Table   tableSummary     `json:"table"`
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Limit   int              `json:"limit"`
	Query   string           `json:"query"`
}

type importedCase struct {
	Paper        models.Paper         `json:"paper"`
	Materials    []models.Material    `json:"materials"`
	Devices      []models.Device      `json:"devices"`
	Measurements []models.Measurement `json:"measurements"`
}

var tableDefs = []tableDef{
	{Name: "papers", Label: "Papers", Model: &models.Paper{}, SearchCols: []string{"doi", "title", "abstract", "journal", "source_api", "download_status"}, DefaultSort: "created_at desc"},
	{Name: "paper_assets", Label: "Paper assets", Model: &models.PaperAsset{}, SearchCols: []string{"paper_id", "asset_type", "source_url", "local_path", "mime_type", "access_type", "error_message"}, DefaultSort: "created_at desc"},
	{Name: "material_classes", Label: "Material classes", Model: &models.MaterialClass{}, SearchCols: []string{"code", "description"}, DefaultSort: "code asc"},
	{Name: "paper_material_classes", Label: "Paper material classes", Model: &models.PaperMaterialClass{}, SearchCols: []string{"paper_id", "assigned_by", "evidence_text"}, DefaultSort: "created_at desc"},
	{Name: "llm_classifications", Label: "LLM classifications", Model: &models.LLMClassification{}, SearchCols: []string{"paper_id", "model_name", "prompt_version", "raw_response", "error_message"}, DefaultSort: "created_at desc"},
	{Name: "crawl_jobs", Label: "Crawl jobs", Model: &models.CrawlJob{}, SearchCols: []string{"query", "source", "status", "error_message"}, DefaultSort: "started_at desc"},
	{Name: "crawl_logs", Label: "Crawl logs", Model: &models.CrawlLog{}, SearchCols: []string{"crawl_job_id", "level", "message", "doi"}, DefaultSort: "created_at desc"},
	{Name: "materials", Label: "Materials", Model: &models.Material{}, SearchCols: []string{"name", "notes"}, DefaultSort: "created_at desc"},
	{Name: "compositions", Label: "Compositions", Model: &models.Composition{}, SearchCols: []string{"material_id", "formula_raw", "normalized"}, DefaultSort: "created_at desc"},
	{Name: "structures", Label: "Structures", Model: &models.Structure{}, SearchCols: []string{"material_id", "phase"}, DefaultSort: "created_at desc"},
	{Name: "devices", Label: "Devices", Model: &models.Device{}, SearchCols: []string{"material_id", "paper_id", "stack"}, DefaultSort: "created_at desc"},
	{Name: "measurements", Label: "Measurements", Model: &models.Measurement{}, SearchCols: []string{"device_id", "paper_id"}, DefaultSort: "created_at desc"},
	{Name: "paper_materials", Label: "Paper materials", SearchCols: []string{"paper_id", "material_id"}, DefaultSort: "paper_id asc"},
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/tables/", s.tableAPI)
	mux.HandleFunc("/api/tables", s.tablesAPI)
	mux.HandleFunc("/download/imported-data2.json", s.downloadImportedData2)
	mux.HandleFunc("/download/tables/", s.downloadTable)
	mux.HandleFunc("/papers/", s.paperDetail)
	mux.HandleFunc("/papers", s.papers)
	mux.HandleFunc("/assets/", s.assetDetail)
	mux.HandleFunc("/", s.index)
	return mux
}

func (s Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s Server) papers(w http.ResponseWriter, r *http.Request) {
	var papers []models.Paper
	q := s.DB.Model(&models.Paper{}).Preload("Classes.MaterialClass").Order("year desc, created_at desc").Limit(200)
	params := r.URL.Query()
	if tag := params.Get("tag"); tag != "" {
		q = q.Joins("JOIN paper_material_classes pmc ON pmc.paper_id = papers.id").
			Joins("JOIN material_classes mc ON mc.id = pmc.material_class_id").
			Where("mc.code = ?", tag)
	}
	if query := params.Get("query"); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		q = q.Where("lower(title) LIKE ? OR lower(abstract) LIKE ? OR lower(journal) LIKE ?", like, like, like)
	}
	if status := params.Get("download_status"); status != "" {
		q = q.Where("download_status = ?", status)
	}
	if year := params.Get("year"); year != "" {
		q = q.Where("year = ?", year)
	}
	if err := q.Find(&papers).Error; err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, papers)
}

func (s Server) paperDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/papers/")
	var paper models.Paper
	if err := s.DB.Preload("Assets").Preload("Classes.MaterialClass").First(&paper, "id = ?", id).Error; err != nil {
		status := http.StatusInternalServerError
		if err == gorm.ErrRecordNotFound {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, paper)
}

func (s Server) assetDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/assets/")
	var asset models.PaperAsset
	if err := s.DB.First(&asset, "id = ?", id).Error; err != nil {
		status := http.StatusInternalServerError
		if err == gorm.ErrRecordNotFound {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, asset)
}

func (s Server) tablesAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/tables" {
		http.NotFound(w, r)
		return
	}
	summaries, err := s.tableSummaries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, summaries)
}

func (s Server) tableAPI(w http.ResponseWriter, r *http.Request) {
	name := path.Base(r.URL.Path)
	def, ok := findTable(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	result, err := s.tableResult(def, r.URL.Query().Get("query"), parseLimit(r.URL.Query().Get("limit"), 100, 500))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

func (s Server) downloadTable(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(path.Base(r.URL.Path), ".json")
	def, ok := findTable(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	result, err := s.tableResult(def, r.URL.Query().Get("query"), parseLimit(r.URL.Query().Get("limit"), 5000, 20000))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeDownloadJSON(w, name+".json", result.Rows)
}

func (s Server) downloadImportedData2(w http.ResponseWriter, r *http.Request) {
	var papers []models.Paper
	if err := s.DB.Where("source_api = ?", "data2").Order("created_at desc").Find(&papers).Error; err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cases := make([]importedCase, 0, len(papers))
	for _, paper := range papers {
		item := importedCase{Paper: paper}
		if err := s.DB.Joins("JOIN paper_materials pm ON pm.material_id = materials.id").
			Where("pm.paper_id = ?", paper.ID).
			Order("materials.created_at desc").
			Find(&item.Materials).Error; err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.DB.Where("paper_id = ?", paper.ID).Order("created_at desc").Find(&item.Devices).Error; err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := s.DB.Where("paper_id = ?", paper.ID).Order("created_at desc").Find(&item.Measurements).Error; err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cases = append(cases, item)
	}
	writeDownloadJSON(w, "imported-data2.json", map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"source":       "data2",
		"count":        len(cases),
		"cases":        cases,
	})
}

func (s Server) tableSummaries() ([]tableSummary, error) {
	summaries := make([]tableSummary, 0, len(tableDefs))
	for _, def := range tableDefs {
		var count int64
		if err := s.DB.Table(def.Name).Count(&count).Error; err != nil {
			return nil, err
		}
		summaries = append(summaries, tableSummary{Name: def.Name, Label: def.Label, Count: count})
	}
	return summaries, nil
}

func (s Server) tableResult(def tableDef, query string, limit int) (tableResult, error) {
	rows, columns, err := s.queryTableRows(def, query, limit)
	if err != nil {
		return tableResult{}, err
	}
	var count int64
	if err := s.DB.Table(def.Name).Count(&count).Error; err != nil {
		return tableResult{}, err
	}
	return tableResult{
		Table:   tableSummary{Name: def.Name, Label: def.Label, Count: count},
		Columns: columns,
		Rows:    rows,
		Limit:   limit,
		Query:   query,
	}, nil
}

func (s Server) queryTableRows(def tableDef, query string, limit int) ([]map[string]any, []string, error) {
	dbq := s.DB.Table(def.Name)
	if query != "" && len(def.SearchCols) > 0 {
		like := "%" + strings.ToLower(query) + "%"
		parts := make([]string, 0, len(def.SearchCols))
		args := make([]any, 0, len(def.SearchCols))
		for _, col := range def.SearchCols {
			parts = append(parts, "lower(CAST("+col+" AS TEXT)) LIKE ?")
			args = append(args, like)
		}
		dbq = dbq.Where(strings.Join(parts, " OR "), args...)
	}
	if def.DefaultSort != "" {
		dbq = dbq.Order(def.DefaultSort)
	}
	sqlRows, err := dbq.Limit(limit).Rows()
	if err != nil {
		return nil, nil, err
	}
	defer sqlRows.Close()

	columns, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, err
	}
	result := []map[string]any{}
	for sqlRows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = normalizeDBValue(values[i])
		}
		result = append(result, row)
	}
	return result, columns, sqlRows.Err()
}

func normalizeDBValue(v any) any {
	switch value := v.(type) {
	case nil:
		return nil
	case []byte:
		if json.Valid(value) {
			return json.RawMessage(value)
		}
		return string(value)
	case time.Time:
		return value.UTC().Format(time.RFC3339)
	default:
		return value
	}
}

func findTable(name string) (tableDef, bool) {
	for _, def := range tableDefs {
		if def.Name == name {
			return def, true
		}
	}
	return tableDef{}, false
}

var indexTemplate = template.Must(template.New("index").Funcs(template.FuncMap{
	"cell": func(row map[string]any, col string) string {
		value, ok := row[col]
		if !ok || value == nil {
			return ""
		}
		switch v := value.(type) {
		case json.RawMessage:
			return compactString(string(v), 160)
		default:
			return compactString(fmt.Sprint(v), 160)
		}
	},
}).Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PVSK Database</title>
<style>
:root{color-scheme:light;background:#f5f6f8;color:#17202a;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
*{box-sizing:border-box}body{margin:0}.shell{display:grid;grid-template-columns:260px minmax(0,1fr);min-height:100vh}
aside{background:#fff;border-right:1px solid #d9dde3;padding:20px 16px;position:sticky;top:0;height:100vh;overflow:auto}
main{min-width:0;padding:24px}.brand{font-size:18px;font-weight:700;margin:0 0 18px}.table-list{display:grid;gap:4px}
.table-link{display:flex;justify-content:space-between;gap:12px;align-items:center;color:#243447;text-decoration:none;border-radius:6px;padding:8px 10px}
.table-link:hover,.table-link.active{background:#e8eef7}.count{color:#637083;font-variant-numeric:tabular-nums}
.toolbar{display:flex;align-items:end;justify-content:space-between;gap:16px;margin-bottom:16px;flex-wrap:wrap}
h1{font-size:26px;line-height:1.2;margin:0 0 4px}.muted{color:#637083;margin:0}.actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
input,select{height:36px;border:1px solid #b9c1cc;border-radius:6px;background:#fff;color:#17202a;padding:0 10px;min-width:130px}
button,.button{height:36px;border:1px solid #1f5eff;background:#1f5eff;color:#fff;border-radius:6px;padding:0 12px;text-decoration:none;display:inline-flex;align-items:center;font-size:14px;cursor:pointer}
.button.secondary{background:#fff;color:#1f5eff}.button.neutral{background:#2d3748;border-color:#2d3748}
.table-wrap{overflow:auto;background:#fff;border:1px solid #d9dde3;border-radius:8px;max-height:calc(100vh - 150px)}
table{border-collapse:collapse;width:100%;min-width:900px;font-size:13px}th,td{border-bottom:1px solid #edf0f3;padding:9px 10px;text-align:left;vertical-align:top}
th{position:sticky;top:0;background:#f9fafb;z-index:1;color:#3a4655;font-weight:650}td{max-width:360px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.empty{background:#fff;border:1px solid #d9dde3;border-radius:8px;padding:28px;color:#637083}
@media(max-width:760px){.shell{grid-template-columns:1fr}aside{position:relative;height:auto;border-right:0;border-bottom:1px solid #d9dde3}.table-wrap{max-height:none}main{padding:18px}.toolbar{align-items:stretch}.actions{width:100%}input,select,button,.button{flex:1}}
</style>
</head>
<body>
<div class="shell">
<aside>
<p class="brand">PVSK Database</p>
<nav class="table-list">
{{range .Tables}}<a class="table-link {{if eq $.Current.Name .Name}}active{{end}}" href="/?table={{.Name}}"><span>{{.Label}}</span><span class="count">{{.Count}}</span></a>{{end}}
</nav>
</aside>
<main>
<div class="toolbar">
<div>
<h1>{{.Current.Label}}</h1>
<p class="muted">{{.Current.Count}} rows · showing up to {{.Result.Limit}}</p>
</div>
<form class="actions" method="get">
<input type="hidden" name="table" value="{{.Current.Name}}">
<input name="query" placeholder="Search table" value="{{.Result.Query}}">
<select name="limit">
{{range .Limits}}<option value="{{.}}" {{if eq $.Result.Limit .}}selected{{end}}>{{.}} rows</option>{{end}}
</select>
<button type="submit">Search</button>
<a class="button secondary" href="/download/tables/{{.Current.Name}}.json?query={{.Result.Query}}&limit=20000">Export table JSON</a>
<a class="button neutral" href="/download/imported-data2.json">Export imported data2 JSON</a>
</form>
</div>
{{if .Result.Rows}}
<div class="table-wrap">
<table>
<thead><tr>{{range .Result.Columns}}<th>{{.}}</th>{{end}}</tr></thead>
<tbody>
{{range .Result.Rows}}<tr>{{ $row := . }}{{range $.Result.Columns}}<td title="{{cell $row .}}">{{cell $row .}}</td>{{end}}</tr>{{end}}
</tbody>
</table>
</div>
{{else}}<div class="empty">No rows found.</div>{{end}}
</main>
</div>
</body>
</html>`))

func (s Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	summaries, err := s.tableSummaries()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	def := tableDefs[0]
	if name := r.URL.Query().Get("table"); name != "" {
		var ok bool
		def, ok = findTable(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
	}
	result, err := s.tableResult(def, r.URL.Query().Get("query"), parseLimit(r.URL.Query().Get("limit"), 100, 500))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		return tableOrder(summaries[i].Name) < tableOrder(summaries[j].Name)
	})
	_ = indexTemplate.Execute(w, map[string]any{
		"Tables":  summaries,
		"Current": result.Table,
		"Result":  result,
		"Limits":  []int{25, 50, 100, 250, 500},
	})
}

func tableOrder(name string) int {
	for i, def := range tableDefs {
		if def.Name == name {
			return i
		}
	}
	return len(tableDefs)
}

func parseLimit(raw string, fallback, max int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}

func compactString(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeDownloadJSON(w http.ResponseWriter, filename string, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
