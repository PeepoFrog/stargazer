package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/PeepFrog/datastsciparser/internal/catalogdb"
	"github.com/PeepFrog/datastsciparser/internal/userdatapath"
)

type mode int

const (
	modeBrowse mode = iota
	modeSearch
)

const (
	boxHorizontalPadding = 1
	boxVerticalBorder    = 2
	boxHorizontalBorder  = 2
)

var qualityCycle = []string{
	"",
	"strong_rgb",
	"weak_rgb",
	"single_filter",
}

type model struct {
	store    *catalogdb.Store
	source   string
	dbPath   string
	pageSize int

	width  int
	height int

	items  []catalogdb.CandidateRecord
	total  int
	offset int
	cursor int

	query   string
	quality string

	searchInput textinput.Model
	mode        mode
	status      string
}

func main() {
	var (
		source   = flag.String("source", "jwst", "data source: jwst|hst")
		pageSize = flag.Int("page-size", 20, "number of rows per page")
	)
	flag.Parse()

	dbPath, err := userdatapath.CatalogDBPath(*source)
	if err != nil {
		log.Fatalf("resolve db path: %v", err)
	}

	store, err := catalogdb.Open(dbPath)
	if err != nil {
		log.Fatalf("open catalog db: %v", err)
	}
	defer store.Close()

	m := newModel(store, strings.ToLower(strings.TrimSpace(*source)), dbPath, *pageSize)
	if err := m.reload(); err != nil {
		log.Fatalf("initial load: %v", err)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("run tui: %v", err)
	}
}

func newModel(store *catalogdb.Store, source, dbPath string, pageSize int) model {
	if pageSize <= 0 {
		pageSize = 20
	}

	ti := textinput.New()
	ti.Placeholder = "Search target name..."
	ti.CharLimit = 256
	ti.Width = 40

	return model{
		store:       store,
		source:      source,
		dbPath:      dbPath,
		pageSize:    pageSize,
		searchInput: ti,
		mode:        modeBrowse,
		status:      "Ready. / search | tab quality | j/k move | n/p page | enter render command | q quit",
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeSearch {
			switch msg.String() {
			case "esc":
				m.mode = modeBrowse
				m.searchInput.Blur()
				m.status = "Search cancelled"
				return m, nil

			case "enter":
				m.query = strings.TrimSpace(m.searchInput.Value())
				m.offset = 0
				m.cursor = 0
				if err := m.reload(); err != nil {
					m.status = fmt.Sprintf("reload error: %v", err)
				} else {
					m.status = fmt.Sprintf("Search applied: %q", m.query)
				}
				m.mode = modeBrowse
				m.searchInput.Blur()
				return m, nil
			}

			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "/":
			m.mode = modeSearch
			m.searchInput.SetValue(m.query)
			m.searchInput.CursorEnd()
			m.searchInput.Focus()
			m.status = "Type search and press Enter"
			return m, nil

		case "tab":
			m.quality = nextQuality(m.quality)
			m.offset = 0
			m.cursor = 0
			if err := m.reload(); err != nil {
				m.status = fmt.Sprintf("reload error: %v", err)
			} else {
				m.status = fmt.Sprintf("Quality filter: %s", displayValue(m.quality))
			}
			return m, nil

		case "r":
			if err := m.reload(); err != nil {
				m.status = fmt.Sprintf("reload error: %v", err)
			} else {
				m.status = "Reloaded"
			}
			return m, nil

		case "j", "down":
			if len(m.items) == 0 {
				return m, nil
			}
			if m.cursor < len(m.items)-1 {
				m.cursor++
				return m, nil
			}
			if m.offset+m.pageSize < m.total {
				m.offset += m.pageSize
				if err := m.reload(); err != nil {
					m.status = fmt.Sprintf("next page error: %v", err)
					return m, nil
				}
				m.cursor = 0
			}
			return m, nil

		case "k", "up":
			if len(m.items) == 0 {
				return m, nil
			}
			if m.cursor > 0 {
				m.cursor--
				return m, nil
			}
			if m.offset > 0 {
				m.offset -= m.pageSize
				if m.offset < 0 {
					m.offset = 0
				}
				if err := m.reload(); err != nil {
					m.status = fmt.Sprintf("prev page error: %v", err)
					return m, nil
				}
				if len(m.items) > 0 {
					m.cursor = len(m.items) - 1
				}
			}
			return m, nil

		case "n", "right", "pgdown":
			if m.offset+m.pageSize < m.total {
				m.offset += m.pageSize
				m.cursor = 0
				if err := m.reload(); err != nil {
					m.status = fmt.Sprintf("next page error: %v", err)
				}
			}
			return m, nil

		case "p", "left", "pgup":
			if m.offset > 0 {
				m.offset -= m.pageSize
				if m.offset < 0 {
					m.offset = 0
				}
				m.cursor = 0
				if err := m.reload(); err != nil {
					m.status = fmt.Sprintf("prev page error: %v", err)
				}
			}
			return m, nil

		case "home":
			m.offset = 0
			m.cursor = 0
			if err := m.reload(); err != nil {
				m.status = fmt.Sprintf("home reload error: %v", err)
			}
			return m, nil

		case "end":
			if m.total > 0 {
				lastPageOffset := ((m.total - 1) / m.pageSize) * m.pageSize
				m.offset = lastPageOffset
				m.cursor = 0
				if err := m.reload(); err != nil {
					m.status = fmt.Sprintf("end reload error: %v", err)
				}
			}
			return m, nil

		case "enter":
			item := m.selected()
			if item == nil {
				return m, nil
			}
			m.status = buildRenderCommand(m.source, item.TargetName)
			return m, nil
		}
	}

	return m, nil
}

func (m *model) reload() error {
	opts := catalogdb.ListCandidatesOptions{
		Source:  m.source,
		Query:   m.query,
		Quality: m.quality,
		Limit:   m.pageSize,
		Offset:  m.offset,
	}

	total, err := m.store.CountCandidatesFiltered(opts)
	if err != nil {
		return err
	}

	items, err := m.store.ListCandidates(opts)
	if err != nil {
		return err
	}

	m.total = total
	m.items = items

	if len(m.items) == 0 {
		m.cursor = 0
		return nil
	}

	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	return nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading TUI..."
	}

	// 1 safe row зверху, щоб UI не прилипав до верхньої межі alt-screen
	safeHeight := m.height - 1
	if safeHeight < 10 {
		safeHeight = 10
	}

	screenOuterWidth := m.width
	if screenOuterWidth < 40 {
		screenOuterWidth = 40
	}

	header := m.renderHeader(boxInnerWidth(screenOuterWidth))
	footer := m.renderFooter(boxInnerWidth(screenOuterWidth))

	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)

	bodyOuterHeight := safeHeight - headerHeight - footerHeight
	if bodyOuterHeight < 6 {
		bodyOuterHeight = 6
	}

	leftOuterWidth, rightOuterWidth := splitOuterWidths(screenOuterWidth)
	leftInnerWidth := boxInnerWidth(leftOuterWidth)
	rightInnerWidth := boxInnerWidth(rightOuterWidth)

	bodyInnerHeight := boxInnerHeight(bodyOuterHeight)
	if bodyInnerHeight < 1 {
		bodyInnerHeight = 1
	}

	listView := lipgloss.NewStyle().
		Width(leftInnerWidth).
		Height(bodyInnerHeight).
		Border(lipgloss.NormalBorder()).
		Padding(0, boxHorizontalPadding).
		Render(m.renderList(bodyInnerHeight))

	detailView := lipgloss.NewStyle().
		Width(rightInnerWidth).
		Height(bodyInnerHeight).
		Border(lipgloss.NormalBorder()).
		Padding(0, boxHorizontalPadding).
		Render(m.renderDetails(bodyInnerHeight))

	body := lipgloss.JoinHorizontal(lipgloss.Top, listView, detailView)

	return "\n" + lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
		footer,
	)
}

func (m model) renderHeader(innerWidth int) string {
	line := fmt.Sprintf(
		"source=%s  total=%d  offset=%d  page_size=%d  quality=%s  query=%q",
		m.source,
		m.total,
		m.offset,
		m.pageSize,
		displayValue(m.quality),
		m.query,
	)

	return lipgloss.NewStyle().
		Width(innerWidth).
		Border(lipgloss.NormalBorder()).
		Padding(0, boxHorizontalPadding).
		Render(line)
}

func (m model) renderList(maxLines int) string {
	if len(m.items) == 0 {
		return "No results"
	}

	linesPerItem := 4
	visibleItems := maxLines / linesPerItem
	if visibleItems < 1 {
		visibleItems = 1
	}

	start, end := listWindow(len(m.items), m.cursor, visibleItems)

	var lines []string
	for i := start; i < end; i++ {
		item := m.items[i]

		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}

		lines = append(lines,
			fmt.Sprintf("%s%s", prefix, item.TargetName),
			fmt.Sprintf(
				"   obs=%s | q=%s | mode=%s | score=%.2f",
				displayValue(item.ObservationID),
				item.Quality,
				item.SelectionMode,
				item.Score,
			),
			fmt.Sprintf(
				"   rgb=%s/%s/%s",
				displayValue(item.RedFilter),
				displayValue(item.GreenFilter),
				displayValue(item.BlueFilter),
			),
			"",
		)
	}

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var b strings.Builder
	for i, line := range lines {
		style := lipgloss.NewStyle()
		if strings.HasPrefix(line, "> ") {
			style = style.Bold(true)
		}
		b.WriteString(style.Render(line))
		if i != len(lines)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}
func (m model) renderDetails(maxLines int) string {
	item := m.selected()
	if item == nil {
		return "No selection"
	}

	lines := []string{
		lipgloss.NewStyle().Bold(true).Render(item.TargetName),
		"",
		fmt.Sprintf("Classification: %s", displayValue(item.TargetClassification)),
		fmt.Sprintf("Observation:    %s", displayValue(item.ObservationID)),
		fmt.Sprintf("Quality:        %s", displayValue(item.Quality)),
		fmt.Sprintf("Mode:           %s", displayValue(item.SelectionMode)),
		fmt.Sprintf("Product:        %s", displayValue(item.ProductKind)),
		fmt.Sprintf("Score:          %.2f", item.Score),
		fmt.Sprintf("Rows:           %d", item.RowsCount),
		"",
		fmt.Sprintf("R: %s", displayValue(item.RedFilter)),
		fmt.Sprintf("G: %s", displayValue(item.GreenFilter)),
		fmt.Sprintf("B: %s", displayValue(item.BlueFilter)),
		"",
		fmt.Sprintf("Filters: %s", displayValue(item.FiltersCSV)),
		"",
		"Render command:",
		buildRenderCommand(m.source, item.TargetName),
		"",
		"Keys:",
		"/ search",
		"tab cycle quality",
		"j/k or arrows move",
		"n/p or pgdn/pgup page",
		"enter show render command",
		"q quit",
	}

	lines = truncateLines(lines, maxLines)
	return strings.Join(lines, "\n")
}
func (m model) renderFooter(innerWidth int) string {
	searchLine := "Search: inactive"
	if m.mode == modeSearch {
		searchLine = "Search: " + m.searchInput.View()
	}

	statusLine := "Status: " + m.status
	dbLine := "DB: " + m.dbPath

	return lipgloss.NewStyle().
		Width(innerWidth).
		Border(lipgloss.NormalBorder()).
		Padding(0, boxHorizontalPadding).
		Render(searchLine + "\n" + statusLine + "\n" + dbLine)
}

func (m model) selected() *catalogdb.CandidateRecord {
	if len(m.items) == 0 {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return &m.items[m.cursor]
}

func nextQuality(current string) string {
	for i, q := range qualityCycle {
		if q == current {
			return qualityCycle[(i+1)%len(qualityCycle)]
		}
	}
	return qualityCycle[0]
}

func buildRenderCommand(source, target string) string {
	return fmt.Sprintf(`go run ./cmd/rendercandidate -source %s -target %q`, source, target)
}

func displayValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}

func splitOuterWidths(total int) (int, int) {
	if total < 20 {
		return total, total
	}

	left := total / 2
	right := total - left
	return left, right
}

func boxInnerWidth(outer int) int {
	inner := outer - boxHorizontalBorder - boxHorizontalPadding*2
	if inner < 1 {
		return 1
	}
	return inner
}

func boxInnerHeight(outer int) int {
	inner := outer - boxVerticalBorder
	if inner < 1 {
		return 1
	}
	return inner
}

func listWindow(totalItems, cursor, visibleItems int) (int, int) {
	if totalItems <= 0 {
		return 0, 0
	}
	if visibleItems >= totalItems {
		return 0, totalItems
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= totalItems {
		cursor = totalItems - 1
	}

	start := cursor - visibleItems/2
	if start < 0 {
		start = 0
	}

	end := start + visibleItems
	if end > totalItems {
		end = totalItems
		start = end - visibleItems
		if start < 0 {
			start = 0
		}
	}

	return start, end
}

func truncateLines(lines []string, maxLines int) []string {
	if maxLines <= 0 {
		return []string{}
	}
	if len(lines) <= maxLines {
		return lines
	}
	if maxLines == 1 {
		return []string{"..."}
	}

	out := make([]string, 0, maxLines)
	out = append(out, lines[:maxLines-1]...)
	out = append(out, "...")
	return out
}
