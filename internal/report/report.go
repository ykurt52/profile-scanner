// Package report renders a profile as a single-file HTML report that opens offline.
package report

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"skillscanner/internal/analyze"
)

//go:embed template.html
var src string

var palette = []string{"#4f6bed", "#e8743b", "#19a979", "#ed4a7b", "#945ecf", "#13a4b4", "#c9a227", "#6c8893", "#d6544e", "#5899da"}

var funcs = template.FuncMap{
	"pct":   func(f float64) string { return fmt.Sprintf("%.1f", f) },
	"pct0":  func(f float64) string { return fmt.Sprintf("%.0f", f) },
	"size":  size,
	"date":  func(t time.Time) string { return t.Local().Format("2006-01-02") },
	"stamp": func(t time.Time) string { return t.Local().Format("2006-01-02 15:04") },
	"color": func(i int) string { return palette[i%len(palette)] },
	"css":   func(s string) template.CSS { return template.CSS(s) },
	"first": func(n int, s []analyze.Lang) []analyze.Lang {
		if len(s) > n {
			return s[:n]
		}
		return s
	},
	"code": func(s []analyze.Lang) []analyze.Lang {
		var out []analyze.Lang
		for _, l := range s {
			if !l.Markup && l.Weight >= 0.05 {
				out = append(out, l)
			}
		}
		return out
	},
	"width": func(v, max float64) string {
		if max <= 0 {
			return "0"
		}
		return fmt.Sprintf("%.1f", math.Max(1, v/max*100))
	},
	"topPct": func(s []analyze.Score) float64 {
		m := 0.0
		for _, x := range s {
			m = math.Max(m, x.Pct)
		}
		return m
	},
	"topWeight": func(s []analyze.Lang) float64 {
		m := 0.0
		for _, x := range s {
			m = math.Max(m, x.Weight)
		}
		return m
	},
	"donut": donut,
	"level": func(c string) string {
		switch c {
		case "high":
			return "hi"
		case "medium":
			return "mid"
		}
		return "lo"
	},
}

type slice struct {
	Name  string
	Color string
	Pct   float64
	Dash  string
	Shift string
}

// donut builds SVG ring slices for the language split (top 7 plus "Other").
func donut(langs []analyze.Lang) []slice {
	const circ = 2 * math.Pi * 40
	var out []slice
	var rest float64
	for _, l := range langs {
		if l.Markup || l.Weight <= 0 {
			continue
		}
		if len(out) < 7 {
			out = append(out, slice{Name: l.Name, Color: l.Color, Pct: l.Weight})
		} else {
			rest += l.Weight
		}
	}
	if rest > 0.05 {
		out = append(out, slice{Name: "Other", Color: "#9aa3ad", Pct: rest})
	}
	acc := 0.0
	for i := range out {
		n := out[i].Pct / 100 * circ
		out[i].Dash = fmt.Sprintf("%.2f %.2f", math.Max(0, n-1.2), circ)
		out[i].Shift = fmt.Sprintf("%.2f", -acc)
		acc += n
	}
	return out
}

func size(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

// Render produces the HTML output.
func Render(p *analyze.Profile) ([]byte, error) {
	t, err := template.New("r").Funcs(funcs).Parse(src)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Write saves the report (HTML + JSON) under dir and returns the HTML path.
func Write(dir string, p *analyze.Profile) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	html, err := Render(p)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("%s_%s", safe(p.User.Login), p.Date.Local().Format("20060102-150405"))
	out := filepath.Join(dir, base+".html")
	if err := os.WriteFile(out, html, 0o644); err != nil {
		return "", err
	}
	if j, err := json.MarshalIndent(p, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, base+".json"), j, 0o644)
	}
	return out, nil
}

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)
}
