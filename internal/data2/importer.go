package data2

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"pvsk-pipeline/internal/models"
	"pvsk-pipeline/internal/util"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Row struct {
	SolarCellStructure    string
	PerovskiteComposition string
	AdditiveAbbreviation  string
	CASNumber             string
	PubChemID             string
	SMILES                string
	MolecularFormula      string
	MolecularWeight       string
	PCEBefore             *float64
	PCEAfter              *float64
	PCEDeltaAbs           *float64
	PCEDeltaRelativePct   *float64
	DOI                   string
	Raw                   map[string]any
}

type Summary struct {
	Rows                 int
	Skipped              int
	PapersInserted       int
	PapersMatched        int
	MaterialsInserted    int
	MaterialsMatched     int
	DevicesInserted      int
	DevicesMatched       int
	MeasurementsInserted int
	MeasurementsUpdated  int
}

func ImportFile(gdb *gorm.DB, path string) (Summary, error) {
	tableRows, err := readTable(path)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Rows: len(tableRows)}
	for i, raw := range tableRows {
		row := mapRow(raw)
		if row.DOI == "" {
			summary.Skipped++
			continue
		}
		if err := gdb.Transaction(func(tx *gorm.DB) error {
			return mergeRow(tx, row, &summary)
		}); err != nil {
			return summary, fmt.Errorf("merge data2 row %d doi=%q: %w", i+2, row.DOI, err)
		}
	}
	return summary, nil
}

func mergeRow(gdb *gorm.DB, row Row, summary *Summary) error {
	paper, inserted, err := upsertPaper(gdb, row)
	if err != nil {
		return err
	}
	if inserted {
		summary.PapersInserted++
	} else {
		summary.PapersMatched++
	}

	material, inserted, err := upsertMaterial(gdb, row)
	if err != nil {
		return err
	}
	if inserted {
		summary.MaterialsInserted++
	} else {
		summary.MaterialsMatched++
	}
	if err := upsertComposition(gdb, material.ID, row); err != nil {
		return err
	}
	if err := gdb.Model(&material).Association("Papers").Append(&paper); err != nil {
		return err
	}

	device, inserted, err := upsertDevice(gdb, paper.ID, material.ID, row)
	if err != nil {
		return err
	}
	if inserted {
		summary.DevicesInserted++
	} else {
		summary.DevicesMatched++
	}

	inserted, err = upsertMeasurement(gdb, paper.ID, device.ID, row)
	if err != nil {
		return err
	}
	if inserted {
		summary.MeasurementsInserted++
	} else {
		summary.MeasurementsUpdated++
	}
	return nil
}

func upsertPaper(gdb *gorm.DB, row Row) (models.Paper, bool, error) {
	doi := util.NormalizeDOI(row.DOI)
	var paper models.Paper
	err := gdb.Where("doi = ?", doi).First(&paper).Error
	if err == nil {
		return paper, false, nil
	}
	if err != gorm.ErrRecordNotFound {
		return paper, false, err
	}
	raw, err := json.Marshal(row.Raw)
	if err != nil {
		return paper, false, err
	}
	paper = models.Paper{
		DOI:              doi,
		TitleHash:        util.TitleHash("data2:" + doi),
		SourceAPI:        "data2",
		RawMetadata:      datatypes.JSON(raw),
		CrawlStatus:      "merged",
		DownloadStatus:   "metadata_only",
		ExtractionStatus: "done",
	}
	return paper, true, gdb.Create(&paper).Error
}

func upsertMaterial(gdb *gorm.DB, row Row) (models.Material, bool, error) {
	name := strings.TrimSpace(row.PerovskiteComposition)
	if name == "" {
		name = "unknown"
	}
	var material models.Material
	err := gdb.Where("name = ?", name).First(&material).Error
	if err == nil {
		return material, false, nil
	}
	if err != gorm.ErrRecordNotFound {
		return material, false, err
	}
	material = models.Material{Name: name, Notes: "Imported from data2 perovskite_composition"}
	return material, true, gdb.Create(&material).Error
}

func upsertComposition(gdb *gorm.DB, materialID string, row Row) error {
	formula := strings.TrimSpace(row.PerovskiteComposition)
	normalized := normalizeComposition(formula)
	if normalized == "" {
		return nil
	}
	var existing models.Composition
	err := gdb.Where("material_id = ? AND normalized = ?", materialID, normalized).First(&existing).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"source":                 "data2",
		"perovskite_composition": formula,
	})
	if err != nil {
		return err
	}
	composition := models.Composition{
		MaterialID:  materialID,
		FormulaRaw:  formula,
		Normalized:  normalized,
		Composition: datatypes.JSON(metadata),
	}
	return gdb.Create(&composition).Error
}

func upsertDevice(gdb *gorm.DB, paperID, materialID string, row Row) (models.Device, bool, error) {
	stack := deviceStack(row)
	metadata, err := json.Marshal(deviceMetadata(row))
	if err != nil {
		return models.Device{}, false, err
	}
	var device models.Device
	err = gdb.Where("paper_id = ? AND material_id = ? AND stack = ?", paperID, materialID, stack).First(&device).Error
	if err == nil {
		device.Metadata = datatypes.JSON(metadata)
		return device, false, gdb.Save(&device).Error
	}
	if err != gorm.ErrRecordNotFound {
		return device, false, err
	}
	device = models.Device{
		PaperID:    paperID,
		MaterialID: materialID,
		Stack:      stack,
		Metadata:   datatypes.JSON(metadata),
	}
	return device, true, gdb.Create(&device).Error
}

func upsertMeasurement(gdb *gorm.DB, paperID, deviceID string, row Row) (bool, error) {
	metadata, err := json.Marshal(measurementMetadata(row))
	if err != nil {
		return false, err
	}
	pce := row.PCEAfter
	if pce == nil {
		pce = row.PCEBefore
	}
	var measurement models.Measurement
	err = gdb.Where("paper_id = ? AND device_id = ?", paperID, deviceID).First(&measurement).Error
	if err == nil {
		measurement.PCE = pce
		measurement.Metadata = datatypes.JSON(metadata)
		return false, gdb.Save(&measurement).Error
	}
	if err != gorm.ErrRecordNotFound {
		return false, err
	}
	measurement = models.Measurement{
		PaperID:  paperID,
		DeviceID: deviceID,
		PCE:      pce,
		Metadata: datatypes.JSON(metadata),
	}
	return true, gdb.Create(&measurement).Error
}

func mapRow(raw tabularRow) Row {
	row := Row{
		SolarCellStructure:    stringValue(raw, "solar_cell_structure"),
		PerovskiteComposition: stringValue(raw, "perovskite_composition"),
		AdditiveAbbreviation:  stringValue(raw, "additive_abbreviation"),
		CASNumber:             stringValue(raw, "cas_number"),
		PubChemID:             stringValue(raw, "pubchem_id"),
		SMILES:                stringValue(raw, "smiles"),
		MolecularFormula:      stringValue(raw, "molecular_formula"),
		MolecularWeight:       stringValue(raw, "molecular_weight"),
		PCEBefore:             floatValue(raw, "pce_before"),
		PCEAfter:              floatValue(raw, "pce_after"),
		PCEDeltaAbs:           floatValue(raw, "pce_delta_abs"),
		PCEDeltaRelativePct:   floatValue(raw, "pce_delta_relative_pct"),
		DOI:                   stringValue(raw, "doi"),
		Raw:                   map[string]any(raw),
	}
	row.DOI = util.NormalizeDOI(row.DOI)
	return row
}

func stringValue(row tabularRow, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.12f", t), "0"), ".")
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func floatValue(row tabularRow, key string) *float64 {
	v, ok := row[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case float64:
		return &t
	case string:
		if t == "" {
			return nil
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return nil
		}
		return &n
	default:
		return nil
	}
}

func normalizeComposition(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "_", "")
	return v
}

func deviceStack(row Row) string {
	structure := strings.TrimSpace(row.SolarCellStructure)
	if structure == "" {
		structure = "unspecified_structure"
	}
	additive := strings.TrimSpace(row.AdditiveAbbreviation)
	if additive == "" {
		return structure
	}
	return structure + " | additive=" + additive
}

func deviceMetadata(row Row) map[string]any {
	return map[string]any{
		"source":                 "data2",
		"solar_cell_structure":   row.SolarCellStructure,
		"additive_abbreviation":  row.AdditiveAbbreviation,
		"cas_number":             row.CASNumber,
		"pubchem_id":             row.PubChemID,
		"smiles":                 row.SMILES,
		"molecular_formula":      row.MolecularFormula,
		"molecular_weight":       row.MolecularWeight,
		"perovskite_composition": row.PerovskiteComposition,
	}
}

func measurementMetadata(row Row) map[string]any {
	return map[string]any{
		"source":                 "data2",
		"pce_before":             row.PCEBefore,
		"pce_after":              row.PCEAfter,
		"pce_delta_abs":          row.PCEDeltaAbs,
		"pce_delta_relative_pct": row.PCEDeltaRelativePct,
		"raw":                    row.Raw,
	}
}
