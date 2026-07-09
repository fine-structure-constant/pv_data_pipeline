package data2

import (
	"archive/zip"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type tabularRow map[string]any

func readTable(filePath string) ([]tabularRow, error) {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".csv":
		return readCSV(filePath)
	case ".xlsx":
		return readXLSX(filePath)
	default:
		return nil, fmt.Errorf("unsupported data2 file extension %q", filepath.Ext(filePath))
	}
}

func readCSV(filePath string) ([]tabularRow, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	headers := normalizeHeaders(records[0])
	var rows []tabularRow
	for _, record := range records[1:] {
		row := make(tabularRow, len(headers))
		for i, header := range headers {
			if header == "" || i >= len(record) {
				continue
			}
			row[header] = typedCell(record[i])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func readXLSX(filePath string) ([]tabularRow, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	sharedStrings, err := parseSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return nil, err
	}
	sheetPath, err := firstSheetPath(files)
	if err != nil {
		return nil, err
	}
	sheetFile := files[sheetPath]
	if sheetFile == nil {
		return nil, fmt.Errorf("worksheet %q not found", sheetPath)
	}
	matrix, err := parseSheet(sheetFile, sharedStrings)
	if err != nil {
		return nil, err
	}
	if len(matrix) == 0 {
		return nil, nil
	}
	headers := normalizeHeaders(matrix[0])
	var rows []tabularRow
	for _, record := range matrix[1:] {
		row := make(tabularRow, len(headers))
		for i, header := range headers {
			if header == "" || i >= len(record) {
				continue
			}
			row[header] = typedCell(record[i])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func normalizeHeaders(headers []string) []string {
	out := make([]string, len(headers))
	for i, header := range headers {
		out[i] = normalizeKey(header)
	}
	return out
}

func normalizeKey(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.ReplaceAll(v, " ", "_")
	v = strings.ReplaceAll(v, "-", "_")
	return v
}

func typedCell(s string) any {
	v := strings.TrimSpace(s)
	if v == "" {
		return nil
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		return n
	}
	return v
}

func parseSharedStrings(f *zip.File) ([]string, error) {
	if f == nil {
		return nil, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	decoder := xml.NewDecoder(rc)
	var values []string
	var b strings.Builder
	inSI := false
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				b.Reset()
				inSI = true
			}
		case xml.EndElement:
			if t.Name.Local == "si" {
				values = append(values, b.String())
				inSI = false
			}
		case xml.CharData:
			if inSI {
				b.Write([]byte(t))
			}
		}
	}
	return values, nil
}

func firstSheetPath(files map[string]*zip.File) (string, error) {
	workbookFile := files["xl/workbook.xml"]
	relsFile := files["xl/_rels/workbook.xml.rels"]
	if workbookFile == nil || relsFile == nil {
		if files["xl/worksheets/sheet1.xml"] != nil {
			return "xl/worksheets/sheet1.xml", nil
		}
		return "", errors.New("xlsx workbook metadata not found")
	}
	sheetRelID, err := firstSheetRelID(workbookFile)
	if err != nil {
		return "", err
	}
	targets, err := workbookRelationships(relsFile)
	if err != nil {
		return "", err
	}
	target := targets[sheetRelID]
	if target == "" {
		return "", fmt.Errorf("worksheet relationship %q not found", sheetRelID)
	}
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = path.Join("xl", target)
	}
	return path.Clean(target), nil
}

func firstSheetRelID(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	decoder := xml.NewDecoder(rc)
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local == "id" {
				return attr.Value, nil
			}
		}
	}
	return "", errors.New("xlsx workbook has no sheets")
}

func workbookRelationships(f *zip.File) (map[string]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	decoder := xml.NewDecoder(rc)
	targets := map[string]string{}
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "Id":
				id = attr.Value
			case "Target":
				target = attr.Value
			}
		}
		if id != "" && target != "" {
			targets[id] = target
		}
	}
	return targets, nil
}

type worksheetXML struct {
	Rows []rowXML `xml:"sheetData>row"`
}

type rowXML struct {
	Cells []cellXML `xml:"c"`
}

type cellXML struct {
	Ref       string          `xml:"r,attr"`
	Type      string          `xml:"t,attr"`
	Value     string          `xml:"v"`
	InlineStr inlineStringXML `xml:"is"`
}

type inlineStringXML struct {
	Text []string `xml:"t"`
}

func parseSheet(f *zip.File, sharedStrings []string) ([][]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var ws worksheetXML
	if err := xml.NewDecoder(rc).Decode(&ws); err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(ws.Rows))
	for _, row := range ws.Rows {
		values := []string{}
		nextCol := 0
		for _, cell := range row.Cells {
			col := columnIndex(cell.Ref)
			if col < 0 {
				col = nextCol
			}
			for len(values) <= col {
				values = append(values, "")
			}
			values[col] = cellValue(cell, sharedStrings)
			nextCol = col + 1
		}
		out = append(out, values)
	}
	return out, nil
}

func cellValue(cell cellXML, sharedStrings []string) string {
	switch cell.Type {
	case "s":
		idx, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err == nil && idx >= 0 && idx < len(sharedStrings) {
			return sharedStrings[idx]
		}
	case "inlineStr":
		return strings.Join(cell.InlineStr.Text, "")
	}
	return strings.TrimSpace(cell.Value)
}

func columnIndex(ref string) int {
	if ref == "" {
		return -1
	}
	col := 0
	seen := false
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		seen = true
		col = col*26 + int(r-'A'+1)
	}
	if !seen {
		return -1
	}
	return col - 1
}
