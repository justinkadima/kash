package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/justin/conterm/internal/config"

	uv "github.com/charmbracelet/ultraviolet"
)

// Modal actions reported back to the App.
type modalAction int

const (
	actNone modalAction = iota
	actSave
	actCancel
	actFetchModels
)

type fieldKind int

const (
	fText fieldKind = iota
	fToggle
)

type modalField struct {
	label string
	kind  fieldKind
	val   string
}

// SettingsModal edits a copy of the config; Save writes it back through
// the App (which persists and rebuilds the client).
type SettingsModal struct {
	fields    []modalField
	sel       int // 0..len(fields)-1, then buttons
	editing   bool
	editPos   int
	models    []string
	modelSel  int
	showModel bool
	err       string

	// hit rects recorded during draw (absolute coords)
	fieldRects []uv.Rectangle
	btnRects   []uv.Rectangle
	modelRects []uv.Rectangle
	rect       uv.Rectangle // modal outer rect
	modelRect  uv.Rectangle // models list rect
}

func newSettingsModal(cfg *config.Config) *SettingsModal {
	ratio := int(cfg.ChatRatio * 100)
	return &SettingsModal{
		fields: []modalField{
			{label: "API base URL", val: cfg.BaseURL},
			{label: "Model", val: cfg.Model},
			{label: "API key (optional)", val: cfg.APIKey},
			{label: "Temperature", val: fmt.Sprintf("%.2f", cfg.Temperature)},
			{label: "Context lines", val: strconv.Itoa(cfg.ContextLines)},
			{label: "Terminal width % (20-70)", val: strconv.Itoa(ratio)},
			{label: "Auto-run commands", val: onOff(cfg.AutoRun)},
			{label: "System prompt", val: cfg.SystemPrompt},
		},
		modelSel: -1,
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

const (
	btnSaveIdx = iota
	btnCancelIdx
	btnModelsIdx
	btnCount
)

func (m *SettingsModal) selIsButton() bool { return m.sel >= len(m.fields) }

func (m *SettingsModal) handleKey(k uv.Key) modalAction {
	ctrl := k.Mod&uv.ModCtrl != 0
	if m.editing {
		switch k.Code {
		case uv.KeyEnter:
			m.editing = false
			return actNone
		case uv.KeyEscape:
			m.editing = false
			return actNone
		case uv.KeyBackspace:
			if m.editPos > 0 {
				r := []rune(m.fields[m.sel].val)
				m.fields[m.sel].val = string(r[:m.editPos-1]) + string(r[m.editPos:])
				m.editPos--
			}
			return actNone
		case uv.KeyDelete:
			r := []rune(m.fields[m.sel].val)
			if m.editPos < len(r) {
				m.fields[m.sel].val = string(r[:m.editPos]) + string(r[m.editPos+1:])
			}
			return actNone
		case uv.KeyLeft:
			if m.editPos > 0 {
				m.editPos--
			}
			return actNone
		case uv.KeyRight:
			if m.editPos < len([]rune(m.fields[m.sel].val)) {
				m.editPos++
			}
			return actNone
		case uv.KeyHome:
			m.editPos = 0
			return actNone
		case uv.KeyEnd:
			m.editPos = len([]rune(m.fields[m.sel].val))
			return actNone
		}
		if ctrl || k.Mod&(uv.ModAlt|uv.ModMeta) != 0 {
			return actNone
		}
		if k.Text != "" {
			r := []rune(m.fields[m.sel].val)
			m.fields[m.sel].val = string(r[:m.editPos]) + k.Text + string(r[m.editPos:])
			m.editPos += len([]rune(k.Text))
		}
		return actNone
	}

	if m.showModel {
		switch k.Code {
		case uv.KeyEscape:
			m.showModel = false
			return actNone
		case uv.KeyUp:
			if m.modelSel > 0 {
				m.modelSel--
			}
			return actNone
		case uv.KeyDown:
			if m.modelSel < len(m.models)-1 {
				m.modelSel++
			}
			return actNone
		case uv.KeyEnter:
			if m.modelSel >= 0 && m.modelSel < len(m.models) {
				m.fields[1].val = m.models[m.modelSel]
			}
			m.showModel = false
			return actNone
		}
		return actNone
	}

	switch k.Code {
	case uv.KeyEscape:
		return actCancel
	case uv.KeyTab:
		m.sel = (m.sel + 1) % (len(m.fields) + btnCount)
		return actNone
	case uv.KeyUp:
		if m.sel > 0 {
			m.sel--
		}
		return actNone
	case uv.KeyDown:
		if m.sel < len(m.fields)+btnCount-1 {
			m.sel++
		}
		return actNone
	case uv.KeyEnter:
		if m.sel < len(m.fields) {
			f := &m.fields[m.sel]
			if f.kind == fToggle {
				f.val = onOff(f.val != "on")
			} else {
				m.editing = true
				m.editPos = len([]rune(f.val))
			}
			return actNone
		}
		switch m.sel - len(m.fields) {
		case btnSaveIdx:
			return actSave
		case btnCancelIdx:
			return actCancel
		case btnModelsIdx:
			return actFetchModels
		}
	case uv.KeyLeft, uv.KeyRight:
		return actNone
	}
	return actNone
}

// apply validates fields and writes them into cfg.
func (m *SettingsModal) apply(cfg *config.Config) error {
	temp, err := strconv.ParseFloat(strings.TrimSpace(m.fields[3].val), 64)
	if err != nil {
		return fmt.Errorf("temperature: %w", err)
	}
	lines, err := strconv.Atoi(strings.TrimSpace(m.fields[4].val))
	if err != nil {
		return fmt.Errorf("context lines: %w", err)
	}
	ratio, err := strconv.Atoi(strings.TrimSpace(m.fields[5].val))
	if err != nil {
		return fmt.Errorf("terminal width: %w", err)
	}
	cfg.BaseURL = strings.TrimSpace(m.fields[0].val)
	cfg.Model = strings.TrimSpace(m.fields[1].val)
	cfg.APIKey = strings.TrimSpace(m.fields[2].val)
	cfg.Temperature = temp
	cfg.ContextLines = lines
	cfg.ChatRatio = float64(ratio) / 100
	cfg.AutoRun = m.fields[6].val == "on"
	cfg.SystemPrompt = m.fields[7].val
	cfg.Normalize()
	if cfg.Model == "" {
		return fmt.Errorf("model is empty")
	}
	return nil
}

// setModels is called when a fetch completes.
func (m *SettingsModal) setModels(models []string, err error) {
	if err != nil {
		m.err = "models: " + err.Error()
		return
	}
	if len(models) == 0 {
		m.err = "no models reported by server"
		return
	}
	m.err = ""
	m.models = models
	m.modelSel = 0
	m.showModel = true
}

// click handles a mouse press at absolute coords.
func (m *SettingsModal) click(x, y int) modalAction {
	if m.showModel && inRect(m.modelRect, x, y) {
		for i, r := range m.modelRects {
			if inRect(r, x, y) {
				m.modelSel = i
				m.fields[1].val = m.models[i]
				m.showModel = false
				return actNone
			}
		}
		return actNone
	}
	for i, r := range m.fieldRects {
		if inRect(r, x, y) {
			m.sel = i
			return actNone
		}
	}
	for i, r := range m.btnRects {
		if inRect(r, x, y) {
			m.sel = len(m.fields) + i
			switch i {
			case btnSaveIdx:
				return actSave
			case btnCancelIdx:
				return actCancel
			case btnModelsIdx:
				return actFetchModels
			}
		}
	}
	return actNone
}

// draw renders the modal centered over area.
func (m *SettingsModal) draw(scr uv.Screen, area uv.Rectangle) {
	m.fieldRects = m.fieldRects[:0]
	m.btnRects = m.btnRects[:0]
	m.modelRects = m.modelRects[:0]

	h := len(m.fields) + 4 // border 2 + fields + buttons + err line
	if m.err != "" {
		h++
	}
	if m.showModel {
		h += min(len(m.models), 6) + 2 // boxed list inside the modal
	}
	h = min(h, area.Dy()-2)
	w := min(64, area.Dx()-2)
	x0 := area.Min.X + (area.Dx()-w)/2
	y0 := area.Min.Y + (area.Dy()-h)/2
	m.rect = uv.Rect(x0, y0, w, h)

	// backing plate + box
	fillRect(scr, m.rect, uv.Style{})
	inner := drawBox(scr, m.rect, stAccent, "settings")

	y := 0
	for i := range m.fields {
		f := &m.fields[i]
		if y >= inner.Dy() {
			break
		}
		labelSt := stDim
		valSt := uv.Style{}
		if m.sel == i {
			labelSt = stAccent
			if m.editing {
				valSt = stYellow
			} else {
				valSt = stGreen
			}
		}
		lx := putStr(scr, inner, 1, y, f.label+":", labelSt)
		val := f.val
		if m.editing && m.sel == i {
			r := []rune(val)
			val = string(r[:m.editPos]) + "│" + string(r[m.editPos:])
		}
		avail := inner.Dx() - 2 - lx
		putStr(scr, inner, lx+1, y, truncate(scr, val, avail), valSt)
		m.fieldRects = append(m.fieldRects, uv.Rect(inner.Min.X, inner.Min.Y+y, inner.Dx(), 1))
		y++
	}

	// buttons row
	if y < inner.Dy()-1 {
		bx := 2
		btn := func(label string, idx int) {
			st := stDim
			if m.sel == len(m.fields)+idx {
				st = stAccent
			}
			end := putStr(scr, inner, bx, y, label, st)
			m.btnRects = append(m.btnRects, uv.Rect(inner.Min.X+bx, inner.Min.Y+y, end-bx, 1))
			bx = end + 4
		}
		btn("save", btnSaveIdx)
		btn("cancel", btnCancelIdx)
		btn("load models", btnModelsIdx)
		y++
	}

	// error line
	if m.err != "" && y < inner.Dy() {
		putStr(scr, inner, 1, y, truncate(scr, m.err, inner.Dx()-2), stRed)
		y++
	}

	// models list (inside the modal plate)
	if m.showModel && len(m.models) > 0 && y < inner.Dy()-1 {
		lh := min(len(m.models), 6, inner.Dy()-y-1)
		lrect := uv.Rect(inner.Min.X, inner.Min.Y+y, inner.Dx(), lh+2)
		m.modelRect = lrect
		lin := drawBox(scr, lrect, stBorder, "models")
		m.modelRects = m.modelRects[:0]
		for i := 0; i < lh; i++ {
			st := uv.Style{}
			if i == m.modelSel {
				st = stReverse
			}
			putStr(scr, lin, 0, i, truncate(scr, m.models[i], lin.Dx()), st)
			m.modelRects = append(m.modelRects, uv.Rect(lin.Min.X, lin.Min.Y+i, lin.Dx(), 1))
		}
	}
}
