package server

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"pvsk-pipeline/internal/models"

	"gorm.io/gorm"
)

type Server struct {
	DB *gorm.DB
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
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

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Perovskite Papers</title>
<style>body{font-family:system-ui,sans-serif;margin:2rem;max-width:1100px}input,select{padding:.4rem;margin-right:.5rem}article{border-bottom:1px solid #ddd;padding:1rem 0}.meta{color:#555;font-size:.9rem}.tag{display:inline-block;background:#eef;border-radius:4px;padding:.1rem .35rem;margin:.1rem}</style></head>
<body><h1>Perovskite Papers</h1>
<form method="get"><input name="query" placeholder="keyword" value="{{.Query}}"><input name="tag" placeholder="tag" value="{{.Tag}}"><input name="download_status" placeholder="download_status" value="{{.Status}}"><button>Search</button></form>
{{range .Papers}}<article><h2>{{.Title}}</h2><div class="meta">{{.Journal}} {{.Year}} · DOI {{.DOI}} · {{.DownloadStatus}}</div><p>{{.Abstract}}</p><p>{{range .Classes}}<span class="tag">{{.MaterialClass.Code}}</span>{{end}}</p></article>{{else}}<p>No papers.</p>{{end}}
</body></html>`))

func (s Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	var papers []models.Paper
	q := s.DB.Model(&models.Paper{}).Preload("Classes.MaterialClass").Order("year desc, created_at desc").Limit(100)
	params := r.URL.Query()
	if tag := params.Get("tag"); tag != "" {
		q = q.Joins("JOIN paper_material_classes pmc ON pmc.paper_id = papers.id").
			Joins("JOIN material_classes mc ON mc.id = pmc.material_class_id").
			Where("mc.code = ?", tag)
	}
	if query := params.Get("query"); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		q = q.Where("lower(title) LIKE ? OR lower(abstract) LIKE ?", like, like)
	}
	if status := params.Get("download_status"); status != "" {
		q = q.Where("download_status = ?", status)
	}
	if err := q.Find(&papers).Error; err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = indexTemplate.Execute(w, map[string]any{
		"Papers": papers, "Query": params.Get("query"), "Tag": params.Get("tag"), "Status": params.Get("download_status"),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
