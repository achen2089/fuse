package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"fuse/internal/domain"
)

type pane int

const (
	nodesPane pane = iota
	jobsPane
	eventsPane
)

var paneOrder = []pane{nodesPane, jobsPane, eventsPane}

type refreshTickMsg struct{}

type contextDoneMsg struct{}

type animationTickMsg struct {
	at time.Time
}

type keyMap struct {
	Quit       key.Binding
	Refresh    key.Binding
	NextPane   key.Binding
	PrevPane   key.Binding
	Command    key.Binding
	Cancel     key.Binding
	Down       key.Binding
	Up         key.Binding
	Top        key.Binding
	Bottom     key.Binding
	ToggleHelp key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		NextPane: key.NewBinding(
			key.WithKeys("tab", "right", "l"),
			key.WithHelp("tab/l", "next pane"),
		),
		PrevPane: key.NewBinding(
			key.WithKeys("shift+tab", "left", "h"),
			key.WithHelp("shift+tab/h", "prev pane"),
		),
		Command: key.NewBinding(
			key.WithKeys(":", "/"),
			key.WithHelp(":", "command"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close prompt"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("j/down", "scroll"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("k/up", "scroll"),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),
		ToggleHelp: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Command, k.Refresh, k.NextPane, k.Down, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Command, k.Cancel, k.Refresh},
		{k.NextPane, k.PrevPane, k.ToggleHelp},
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.Quit},
	}
}

type styles struct {
	canvas       lipgloss.Style
	header       lipgloss.Style
	headerTitle  lipgloss.Style
	headerMeta   lipgloss.Style
	infoBadge    lipgloss.Style
	mutedBadge   lipgloss.Style
	tabActive    lipgloss.Style
	tabMuted     lipgloss.Style
	focusedPane  lipgloss.Style
	pane         lipgloss.Style
	paneTitle    lipgloss.Style
	paneTitleDim lipgloss.Style
	label        lipgloss.Style
	value        lipgloss.Style
	muted        lipgloss.Style
	goodPill     lipgloss.Style
	warnPill     lipgloss.Style
	badPill      lipgloss.Style
	neutralPill  lipgloss.Style
	barLabel     lipgloss.Style
	errorBanner  lipgloss.Style
	staleBanner  lipgloss.Style
	empty        lipgloss.Style
	commandBar   lipgloss.Style
	commandLive  lipgloss.Style
	commandHint  lipgloss.Style
	commandError lipgloss.Style
	commandLabel lipgloss.Style
	help         lipgloss.Style
	card         lipgloss.Style
	cardLabel    lipgloss.Style
	cardValue    lipgloss.Style
}

func newStyles() styles {
	border := lipgloss.RoundedBorder()
	return styles{
		canvas:       lipgloss.NewStyle().Padding(0, 1),
		header:       lipgloss.NewStyle().Background(lipgloss.Color("#0B1220")).Foreground(lipgloss.Color("#F8FAFC")).Padding(0, 1),
		headerTitle:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true),
		headerMeta:   lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
		infoBadge:    lipgloss.NewStyle().Foreground(lipgloss.Color("#D1FAE5")).Background(lipgloss.Color("#064E3B")).Padding(0, 1),
		mutedBadge:   lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Background(lipgloss.Color("#172033")).Padding(0, 1),
		tabActive:    lipgloss.NewStyle().Foreground(lipgloss.Color("#E0F2FE")).Background(lipgloss.Color("#0F3C5B")).Padding(0, 1).Bold(true),
		tabMuted:     lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Background(lipgloss.Color("#172033")).Padding(0, 1),
		focusedPane:  lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("#38BDF8")).Background(lipgloss.Color("#0B1220")).Padding(0, 1),
		pane:         lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("#334155")).Background(lipgloss.Color("#0A101D")).Padding(0, 1),
		paneTitle:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true),
		paneTitleDim: lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")),
		label:        lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
		value:        lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true),
		muted:        lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
		goodPill:     lipgloss.NewStyle().Foreground(lipgloss.Color("#DCFCE7")).Background(lipgloss.Color("#166534")).Padding(0, 1),
		warnPill:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FEF3C7")).Background(lipgloss.Color("#92400E")).Padding(0, 1),
		badPill:      lipgloss.NewStyle().Foreground(lipgloss.Color("#FEE2E2")).Background(lipgloss.Color("#991B1B")).Padding(0, 1),
		neutralPill:  lipgloss.NewStyle().Foreground(lipgloss.Color("#DBEAFE")).Background(lipgloss.Color("#1D4ED8")).Padding(0, 1),
		barLabel:     lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Bold(true),
		errorBanner:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FEE2E2")).Background(lipgloss.Color("#7F1D1D")).Padding(0, 1),
		staleBanner:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FEF3C7")).Background(lipgloss.Color("#78350F")).Padding(0, 1),
		empty:        lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Italic(true),
		commandBar:   lipgloss.NewStyle().Background(lipgloss.Color("#0B1220")).Foreground(lipgloss.Color("#E2E8F0")).Padding(0, 1),
		commandLive:  lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")),
		commandHint:  lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
		commandError: lipgloss.NewStyle().Foreground(lipgloss.Color("#FCA5A5")),
		commandLabel: lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true),
		help:         lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
		card:         lipgloss.NewStyle().Border(border).BorderForeground(lipgloss.Color("#233043")).Padding(0, 1),
		cardLabel:    lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")),
		cardValue:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true),
	}
}

type model struct {
	ctx            context.Context
	cli            Client
	opts           Options
	keys           keyMap
	help           help.Model
	spinner        spinner.Model
	styles         styles
	width          int
	height         int
	showHelp       bool
	loading        bool
	refreshing     bool
	commandMode    bool
	selected       pane
	data           snapshot
	hasSnapshot    bool
	lastErr        error
	lastAttemptAt  time.Time
	commandInput   textinput.Model
	commandStatus  string
	commandErrored bool
	now            time.Time
	pulseFrame     int
	nodesViewport  viewport.Model
	jobsViewport   viewport.Model
	eventsViewport viewport.Model
}

func newModel(ctx context.Context, cli Client, opts Options) *model {
	spin := spinner.New()
	spin.Spinner = spinner.Line
	spin.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8"))
	h := help.New()
	h.ShowAll = false
	input := textinput.New()
	input.Placeholder = "nodes | jobs | events | refresh | help | quit"
	input.CharLimit = 120
	input.Prompt = ""
	input.Blur()

	return &model{
		ctx:          ctx,
		cli:          cli,
		opts:         opts,
		keys:         newKeyMap(),
		help:         h,
		spinner:      spin,
		styles:       newStyles(),
		loading:      true,
		refreshing:   false,
		commandInput: input,
		selected:     nodesPane,
		now:          time.Now(),
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchSnapshotCmd(m.ctx, m.cli, m.opts.EventLimit),
		waitForContextCmd(m.ctx),
		scheduleAnimation(),
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.commandInput.Width = max(12, msg.Width-34)
		m.layoutViewports()
		m.syncViewportContent()
		return m, nil
	case spinner.TickMsg:
		if !m.loading && !m.refreshing {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case animationTickMsg:
		m.now = msg.at
		m.pulseFrame = (m.pulseFrame + 1) % 32
		return m, scheduleAnimation()
	case refreshTickMsg:
		if m.refreshing {
			return m, nil
		}
		m.refreshing = true
		return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd(m.ctx, m.cli, m.opts.EventLimit))
	case snapshotResultMsg:
		m.refreshing = false
		m.loading = false
		m.lastAttemptAt = msg.attemptedAt
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				return m, tea.Quit
			}
			m.lastErr = msg.err
			return m, scheduleRefresh(m.opts.RefreshInterval)
		}
		m.lastErr = nil
		m.data = msg.snapshot
		m.hasSnapshot = true
		m.syncViewportContent()
		return m, scheduleRefresh(m.opts.RefreshInterval)
	case contextDoneMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		if m.commandMode {
			switch {
			case key.Matches(msg, m.keys.Quit):
				return m, tea.Quit
			case key.Matches(msg, m.keys.Cancel):
				m.closeCommandMode()
				return m, nil
			case msg.Type == tea.KeyEnter:
				return m, m.executeCommand(m.commandInput.Value())
			}
			var cmd tea.Cmd
			m.commandInput, cmd = m.commandInput.Update(msg)
			return m, cmd
		}
		if msg.String() == ":" || msg.String() == "/" {
			m.openCommandMode("")
			return m, textinput.Blink
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.ToggleHelp):
			m.showHelp = !m.showHelp
			return m, nil
		case key.Matches(msg, m.keys.Refresh):
			if m.refreshing {
				return m, nil
			}
			m.refreshing = true
			return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd(m.ctx, m.cli, m.opts.EventLimit))
		case key.Matches(msg, m.keys.NextPane):
			m.selected = cyclePane(m.selected, 1)
			return m, nil
		case key.Matches(msg, m.keys.PrevPane):
			m.selected = cyclePane(m.selected, -1)
			return m, nil
		case key.Matches(msg, m.keys.Down):
			m.activeViewport().LineDown(1)
			return m, nil
		case key.Matches(msg, m.keys.Up):
			m.activeViewport().LineUp(1)
			return m, nil
		case key.Matches(msg, m.keys.Top):
			m.activeViewport().GotoTop()
			return m, nil
		case key.Matches(msg, m.keys.Bottom):
			m.activeViewport().GotoBottom()
			return m, nil
		}
	}
	return m, nil
}

func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading Fuse TUI..."
	}
	if m.width < m.opts.MinWidth || m.height < m.opts.MinHeight {
		return m.styles.canvas.Render(m.renderTooSmall())
	}

	sections := []string{m.renderHeader(), m.renderTabs()}
	if banner := m.renderBanner(); banner != "" {
		sections = append(sections, banner)
	}

	switch {
	case !m.hasSnapshot && m.lastErr != nil:
		sections = append(sections, m.renderInitialError())
	case m.width < 110:
		sections = append(sections, m.renderNarrowLayout())
	default:
		sections = append(sections, m.renderWideLayout())
	}

	sections = append(sections, m.renderCommandBar())
	if footer := m.renderHelp(); footer != "" {
		sections = append(sections, footer)
	}
	return m.styles.canvas.Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

func (m *model) renderHeader() string {
	statusBadge := m.styles.infoBadge.Render(m.opts.SourceLabel)
	paneBadge := m.styles.neutralPill.Render("pane " + strings.ToLower(m.activePaneTitle()))
	refreshState := "steady"
	if m.loading {
		refreshState = m.spinner.View() + " booting"
	} else if m.refreshing {
		refreshState = m.spinner.View() + " refreshing"
	}
	updated := "snapshot pending"
	if m.hasSnapshot {
		updated = "age " + m.snapshotAgeLabel()
	}
	meta := joinHorizontalWithGap(1,
		statusBadge,
		paneBadge,
		m.styles.mutedBadge.Render(refreshState),
		m.styles.mutedBadge.Render(updated),
	)
	topLine := flexLine(max(0, m.width-2), m.styles.headerTitle.Render(m.opts.Title), meta)

	totalDevices := max(1, max(m.data.Status.Devices, m.totalDeviceCapacity()))
	capacityLine := joinHorizontalWithGap(1,
		m.styles.barLabel.Render("cluster"),
		barGauge(m.data.Status.Allocated, totalDevices, clamp(max(16, m.width/5), 16, 28), m.pulseFrame, m.refreshing),
		m.styles.headerMeta.Render(fmt.Sprintf("%d/%d gpu  %d running  %d pending  %d failed",
			m.data.Status.Allocated, totalDevices, m.data.Status.RunningJobs, m.data.Status.PendingJobs, m.data.Status.FailedJobs,
		)),
	)

	content := lipgloss.JoinVertical(lipgloss.Left, topLine, capacityLine)
	return m.styles.header.Width(max(0, m.width-2)).Render(content)
}

func (m *model) renderTabs() string {
	tabs := []string{
		m.renderTab(nodesPane, "Nodes", len(m.data.Nodes)),
		m.renderTab(jobsPane, "Jobs", len(m.data.Jobs)),
		m.renderTab(eventsPane, "Events", len(m.data.Events)),
	}
	return joinHorizontalWithGap(1, tabs...)
}

func (m *model) renderBanner() string {
	if m.lastErr == nil {
		return ""
	}
	message := truncateText(m.lastErr.Error(), max(20, m.width-18))
	if m.hasSnapshot {
		prefix := "stale snapshot"
		if !m.data.CapturedAt.IsZero() {
			prefix = fmt.Sprintf("stale snapshot from %s", m.data.CapturedAt.Local().Format("15:04:05"))
		}
		return m.styles.staleBanner.Width(max(0, m.width-2)).Render(prefix + " | " + message)
	}
	return m.styles.errorBanner.Width(max(0, m.width-2)).Render("initial load failed | " + message)
}

func (m *model) renderWideLayout() string {
	totalWidth := max(0, m.width-2)
	overviewHeight, lowerHeight := m.heroHeights(1)
	overview := m.renderPane("Cluster Overview", totalWidth, overviewHeight, false, m.renderClusterOverview(totalWidth-6))

	if totalWidth >= 145 {
		widths := splitWidths(totalWidth, 2, 3)
		nodes := m.renderPane("Nodes", widths[0], lowerHeight, m.selected == nodesPane, m.nodesViewport.View())
		jobs := m.renderPane("Jobs", widths[1], lowerHeight, m.selected == jobsPane, m.jobsViewport.View())
		events := m.renderPane("Events", widths[2], lowerHeight, m.selected == eventsPane, m.eventsViewport.View())
		bottom := joinHorizontalWithGap(2, nodes, jobs, events)
		return lipgloss.JoinVertical(lipgloss.Left, overview, bottom)
	}

	leftWidth, rightWidth := splitWidth(totalWidth, 2)
	nodes := m.renderPane("Nodes", leftWidth, lowerHeight, m.selected == nodesPane, m.nodesViewport.View())
	jobsHeight := max(6, lowerHeight/2)
	eventsHeight := max(6, lowerHeight-jobsHeight)
	jobs := m.renderPane("Jobs", rightWidth, jobsHeight, m.selected == jobsPane, m.jobsViewport.View())
	events := m.renderPane("Events", rightWidth, eventsHeight, m.selected == eventsPane, m.eventsViewport.View())
	right := lipgloss.JoinVertical(lipgloss.Left, jobs, events)
	return lipgloss.JoinVertical(lipgloss.Left, overview, joinHorizontalWithGap(2, nodes, right))
}

func (m *model) renderNarrowLayout() string {
	available := max(6, m.height-m.chromeHeight())
	overviewHeight := min(max(10, available/3), max(12, available-8))
	panelHeight := max(6, available-overviewHeight)
	summary := m.renderPane("Cluster Overview", max(0, m.width-2), overviewHeight, false, m.renderClusterOverview(max(0, m.width-6)))
	activeTitle, activeBody := m.activePaneView()
	panel := m.renderPane(activeTitle, max(0, m.width-2), panelHeight, true, activeBody)
	return lipgloss.JoinVertical(lipgloss.Left, summary, panel)
}

func (m *model) renderPane(title string, width, height int, focused bool, body string) string {
	if height < 3 {
		height = 3
	}
	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		m.styles.paneTitle.Render(title),
		" ",
		m.styles.paneTitleDim.Render(m.focusLabel(focused)),
	)
	contentHeight := max(1, height-4)
	trimmed := lipgloss.NewStyle().Width(max(0, width-4)).Height(contentHeight).Render(fitLines(body, contentHeight))
	style := m.styles.pane
	if focused {
		style = m.styles.focusedPane
	}
	style = style.Width(width - 2).Height(height - 2)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, header, "", trimmed))
}

func (m *model) renderSummaryCards(width int) string {
	if !m.hasSnapshot {
		return m.styles.empty.Render("Waiting for the first cluster snapshot.")
	}
	metrics := []metricCard{
		{Label: "Nodes", Value: fmt.Sprintf("%d", m.data.Status.Nodes)},
		{Label: "Devices", Value: fmt.Sprintf("%d", m.data.Status.Devices)},
		{Label: "Allocated", Value: fmt.Sprintf("%d", m.data.Status.Allocated)},
		{Label: "Idle", Value: fmt.Sprintf("%d", m.data.Status.Idle)},
		{Label: "Running", Value: fmt.Sprintf("%d", m.data.Status.RunningJobs)},
		{Label: "Pending", Value: fmt.Sprintf("%d", m.data.Status.PendingJobs)},
		{Label: "Failed", Value: fmt.Sprintf("%d", m.data.Status.FailedJobs)},
	}
	columns := clamp(width/18, 2, 4)
	cardWidth := max(12, (width-(columns-1)*1)/columns)
	rows := make([]string, 0, (len(metrics)+columns-1)/columns)
	for start := 0; start < len(metrics); start += columns {
		end := min(len(metrics), start+columns)
		line := make([]string, 0, end-start)
		for _, metric := range metrics[start:end] {
			line = append(line, m.renderMetricCard(metric, cardWidth))
		}
		rows = append(rows, joinHorizontalWithGap(1, line...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *model) renderClusterOverview(width int) string {
	if !m.hasSnapshot {
		return m.styles.empty.Render("Waiting for the first cluster snapshot.")
	}

	capacityLine := joinHorizontalWithGap(1,
		m.styles.barLabel.Render("capacity"),
		barGauge(m.data.Status.Allocated, max(1, max(m.data.Status.Devices, m.totalDeviceCapacity())), clamp(max(12, width/4), 12, 24), m.pulseFrame, m.refreshing),
		m.styles.headerMeta.Render(fmt.Sprintf("%d/%d gpu", m.data.Status.Allocated, max(1, max(m.data.Status.Devices, m.totalDeviceCapacity())))),
	)

	healthLine := joinHorizontalWithGap(1,
		m.styles.goodPill.Render(fmt.Sprintf("%d healthy", m.countNodeHealth(domain.HealthHealthy))),
		m.styles.warnPill.Render(fmt.Sprintf("%d degraded", m.countNodeHealth(domain.HealthDegraded))),
		m.styles.badPill.Render(fmt.Sprintf("%d offline", m.countNodeHealth(domain.HealthOffline))),
		m.styles.mutedBadge.Render(fmt.Sprintf("hottest %s", m.hottestNodeLabel())),
		m.styles.mutedBadge.Render(fmt.Sprintf("util %s", m.averageUtilLabel())),
	)

	workloadTotal := max(1, m.data.Status.RunningJobs+m.data.Status.PendingJobs+m.data.Status.FailedJobs)
	workloadLine := joinHorizontalWithGap(1,
		m.styles.barLabel.Render("workload"),
		barGauge(m.data.Status.RunningJobs, workloadTotal, clamp(max(12, width/4), 12, 24), m.pulseFrame, m.refreshing),
		m.styles.headerMeta.Render(fmt.Sprintf("%d running  %d pending  %d failed",
			m.data.Status.RunningJobs, m.data.Status.PendingJobs, m.data.Status.FailedJobs,
		)),
	)

	latestEvent := "no recent events"
	if len(m.data.Events) > 0 {
		latestEvent = m.data.Events[0].Time.Local().Format("15:04:05") + " " + string(m.data.Events[0].Reason) + " " + m.data.Events[0].Summary
	}

	summaryLine := joinHorizontalWithGap(1,
		m.styles.mutedBadge.Render(fmt.Sprintf("nodes %d", m.data.Status.Nodes)),
		m.styles.mutedBadge.Render(fmt.Sprintf("devices %d", m.data.Status.Devices)),
		m.styles.mutedBadge.Render(fmt.Sprintf("idle %d", m.data.Status.Idle)),
		m.styles.mutedBadge.Render(fmt.Sprintf("events %d", len(m.data.Events))),
	)

	lines := []string{
		m.styles.headerMeta.Render("Cluster pulse"),
		capacityLine,
		workloadLine,
		healthLine,
		summaryLine,
		truncateText("latest "+latestEvent, max(20, width)),
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *model) renderMetricCard(metric metricCard, width int) string {
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		m.styles.cardLabel.Render(metric.Label),
		m.styles.cardValue.Render(metric.Value),
	)
	return m.styles.card.Width(width - 2).Height(3).Render(body)
}

func (m *model) renderHelp() string {
	m.help.ShowAll = m.showHelp
	footer := m.help.View(m.keys)
	if footer == "" {
		return ""
	}
	return m.styles.help.Width(max(0, m.width-2)).Render(footer)
}

func (m *model) renderCommandBar() string {
	width := max(0, m.width-2)
	if m.commandMode {
		label := m.styles.commandLabel.Render(":")
		content := flexLine(width, joinHorizontalWithGap(1, label, m.commandInput.View()), m.styles.commandHint.Render("enter submit • esc cancel"))
		return m.styles.commandBar.Width(width).Render(content)
	}

	status := m.styles.commandHint.Render("Press : for commands")
	if m.commandStatus != "" {
		statusStyle := m.styles.commandLive
		if m.commandErrored {
			statusStyle = m.styles.commandError
		}
		status = statusStyle.Render(truncateText(m.commandStatus, max(12, width/2)))
	}
	hint := m.styles.commandHint.Render("try nodes, jobs, events, refresh, help, quit")
	return m.styles.commandBar.Width(width).Render(flexLine(width, status, hint))
}

func (m *model) renderInitialError() string {
	lines := []string{
		m.styles.value.Render("Cluster snapshot unavailable"),
		"",
		m.styles.muted.Render("The first refresh failed, so there is no cached snapshot to show yet."),
		m.styles.muted.Render("Press r to retry or wait for the next automatic refresh."),
	}
	if m.lastAttemptAt.IsZero() {
		return m.renderPane("Connection", max(0, m.width-2), max(8, m.height-m.chromeHeight()), true, lipgloss.JoinVertical(lipgloss.Left, lines...))
	}
	lines = append(lines, "", m.styles.muted.Render("Last attempt: "+m.lastAttemptAt.Local().Format("15:04:05")))
	return m.renderPane("Connection", max(0, m.width-2), max(8, m.height-m.chromeHeight()), true, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *model) renderTooSmall() string {
	lines := []string{
		m.styles.value.Render("Fuse TUI needs a little more room."),
		"",
		fmt.Sprintf("Current size: %dx%d", m.width, m.height),
		fmt.Sprintf("Minimum size: %dx%d", m.opts.MinWidth, m.opts.MinHeight),
		"",
		"Stretch the terminal or use a wider split.",
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *model) layoutViewports() {
	if m.width == 0 || m.height == 0 {
		return
	}
	if m.width < m.opts.MinWidth || m.height < m.opts.MinHeight {
		return
	}

	if m.width < 110 {
		width := max(1, m.width-6)
		available := max(6, m.height-m.chromeHeight())
		overviewHeight := min(max(10, available/3), max(12, available-8))
		panelHeight := max(4, available-overviewHeight-4)
		m.nodesViewport.Width = width
		m.jobsViewport.Width = width
		m.eventsViewport.Width = width
		m.nodesViewport.Height = panelHeight
		m.jobsViewport.Height = panelHeight
		m.eventsViewport.Height = panelHeight
		return
	}

	totalWidth := max(0, m.width-2)
	_, lowerHeight := m.heroHeights(1)
	if totalWidth >= 145 {
		widths := splitWidths(totalWidth, 2, 3)
		m.nodesViewport.Width = max(1, widths[0]-6)
		m.jobsViewport.Width = max(1, widths[1]-6)
		m.eventsViewport.Width = max(1, widths[2]-6)
		m.nodesViewport.Height = max(1, lowerHeight-4)
		m.jobsViewport.Height = max(1, lowerHeight-4)
		m.eventsViewport.Height = max(1, lowerHeight-4)
		return
	}

	leftWidth, rightWidth := splitWidth(totalWidth, 2)
	jobsHeight := max(6, lowerHeight/2)
	eventsHeight := max(6, lowerHeight-jobsHeight)
	m.nodesViewport.Width = max(1, leftWidth-6)
	m.jobsViewport.Width = max(1, rightWidth-6)
	m.eventsViewport.Width = max(1, rightWidth-6)
	m.nodesViewport.Height = max(1, lowerHeight-4)
	m.jobsViewport.Height = max(1, jobsHeight-4)
	m.eventsViewport.Height = max(1, eventsHeight-4)
}

func (m *model) syncViewportContent() {
	if !m.hasSnapshot {
		return
	}
	m.nodesViewport.SetContent(m.renderNodesRows())
	m.jobsViewport.SetContent(m.renderJobsRows())
	m.eventsViewport.SetContent(m.renderEventsRows())
}

func (m *model) renderNodesRows() string {
	if len(m.data.Nodes) == 0 {
		return m.styles.empty.Render("No nodes discovered.")
	}
	lines := make([]string, 0, len(m.data.Nodes))
	barWidth := clamp(m.nodesViewport.Width/5, 8, 16)
	for _, node := range m.data.Nodes {
		name := padRight(truncateText(node.Name, 16), 16)
		bar := barGauge(node.Allocated, max(node.TotalGPUs, node.DeviceCount), barWidth, m.pulseFrame, m.selected == nodesPane)
		gpu := fmt.Sprintf("%2d/%-2d", node.Allocated, max(node.TotalGPUs, node.DeviceCount))
		sw := padRight(truncateText(node.SwitchName, 10), 10)
		state := stateBadge(node.State)
		health := healthBadge(node.Health)
		temp := "--"
		if node.MaxTempC > 0 {
			temp = fmt.Sprintf("%dC", node.MaxTempC)
		}
		util := "--"
		if node.AverageUtil > 0 {
			util = fmt.Sprintf("%d%%", node.AverageUtil)
		}
		real := ""
		if node.Real {
			real = m.styles.muted.Render(" real")
		}
		line := lipgloss.JoinHorizontal(
			lipgloss.Center,
			name,
			" ",
			bar,
			" ",
			m.styles.muted.Render(gpu),
			" ",
			m.styles.muted.Render(sw),
			" ",
			state,
			" ",
			health,
			" ",
			m.styles.muted.Render(temp),
			" ",
			m.styles.muted.Render(util),
			real,
		)
		lines = append(lines, truncateStyled(line, max(1, m.nodesViewport.Width)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderJobsRows() string {
	if len(m.data.Jobs) == 0 {
		return m.styles.empty.Render("No jobs in the current snapshot.")
	}
	lines := make([]string, 0, len(m.data.Jobs))
	nameWidth := clamp(m.jobsViewport.Width/3, 12, 24)
	nodeWidth := clamp(m.jobsViewport.Width/4, 8, 22)
	for _, job := range m.data.Jobs {
		name := padRight(truncateText(job.Name, nameWidth), nameWidth)
		team := truncateText(job.Team, 10)
		nodes := truncateText(job.NodeSummary, nodeWidth)
		if nodes == "" {
			nodes = "-"
		}
		line := lipgloss.JoinHorizontal(
			lipgloss.Center,
			jobStateBadge(job.State),
			" ",
			name,
			" ",
			m.styles.mutedBadge.Render(team),
			" ",
			m.styles.muted.Render(fmt.Sprintf("%2d gpu", job.GPUs)),
			" ",
			m.styles.muted.Render("slurm="+job.SlurmID),
			" ",
			m.styles.muted.Render(nodes),
		)
		lines = append(lines, truncateStyled(line, max(1, m.jobsViewport.Width)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderEventsRows() string {
	if len(m.data.Events) == 0 {
		return m.styles.empty.Render("No recent events.")
	}
	lines := make([]string, 0, len(m.data.Events))
	for _, event := range m.data.Events {
		line := lipgloss.JoinHorizontal(
			lipgloss.Center,
			m.styles.muted.Render(event.Time.Local().Format("15:04:05")),
			" ",
			reasonBadge(event.Reason),
			" ",
			truncateText(event.Summary, max(8, m.eventsViewport.Width-22)),
		)
		lines = append(lines, truncateStyled(line, max(1, m.eventsViewport.Width)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) chromeHeight() int {
	height := 2
	if tabs := m.renderTabs(); tabs != "" {
		height += lipgloss.Height(tabs)
	}
	if m.lastErr != nil {
		height++
	}
	if bar := m.renderCommandBar(); bar != "" {
		height += lipgloss.Height(bar)
	}
	if footer := m.renderHelp(); footer != "" {
		height += lipgloss.Height(footer)
	}
	return height + 2
}

func (m *model) activeViewport() *viewport.Model {
	switch m.selected {
	case jobsPane:
		return &m.jobsViewport
	case eventsPane:
		return &m.eventsViewport
	default:
		return &m.nodesViewport
	}
}

func (m *model) activePaneView() (string, string) {
	switch m.selected {
	case jobsPane:
		return "Jobs", m.jobsViewport.View()
	case eventsPane:
		return "Events", m.eventsViewport.View()
	default:
		return "Nodes", m.nodesViewport.View()
	}
}

func (m *model) activePaneTitle() string {
	title, _ := m.activePaneView()
	return title
}

func (m *model) openCommandMode(initial string) {
	m.commandMode = true
	m.commandInput.SetValue(initial)
	m.commandInput.CursorEnd()
	m.commandInput.Focus()
}

func (m *model) closeCommandMode() {
	m.commandMode = false
	m.commandInput.Blur()
	m.commandInput.SetValue("")
}

func (m *model) executeCommand(raw string) tea.Cmd {
	command := strings.TrimSpace(strings.TrimLeft(raw, ":/"))
	m.closeCommandMode()
	if command == "" {
		m.commandStatus = "Command cancelled"
		m.commandErrored = false
		return nil
	}

	fields := strings.Fields(command)
	head := strings.ToLower(fields[0])
	args := fields[1:]

	switch head {
	case "nodes":
		m.selected = nodesPane
		m.commandStatus = "Focused nodes pane"
		m.commandErrored = false
		return nil
	case "jobs":
		m.selected = jobsPane
		m.commandStatus = "Focused jobs pane"
		m.commandErrored = false
		return nil
	case "events":
		m.selected = eventsPane
		m.commandStatus = "Focused events pane"
		m.commandErrored = false
		return nil
	case "pane", "focus":
		if len(args) == 0 {
			m.commandStatus = "Missing pane name"
			m.commandErrored = true
			return nil
		}
		switch strings.ToLower(args[0]) {
		case "nodes":
			m.selected = nodesPane
			m.commandStatus = "Focused nodes pane"
			m.commandErrored = false
		case "jobs":
			m.selected = jobsPane
			m.commandStatus = "Focused jobs pane"
			m.commandErrored = false
		case "events":
			m.selected = eventsPane
			m.commandStatus = "Focused events pane"
			m.commandErrored = false
		default:
			m.commandStatus = "Unknown pane: " + args[0]
			m.commandErrored = true
		}
		return nil
	case "refresh", "reload":
		if m.refreshing {
			m.commandStatus = "Refresh already in progress"
			m.commandErrored = false
			return nil
		}
		m.refreshing = true
		m.commandStatus = "Refreshing cluster snapshot"
		m.commandErrored = false
		return tea.Batch(m.spinner.Tick, fetchSnapshotCmd(m.ctx, m.cli, m.opts.EventLimit))
	case "help":
		m.showHelp = true
		m.commandStatus = "Expanded help"
		m.commandErrored = false
		return nil
	case "hidehelp":
		m.showHelp = false
		m.commandStatus = "Collapsed help"
		m.commandErrored = false
		return nil
	case "status", "overview", "home":
		m.commandStatus = "Overview is always visible above the active pane"
		m.commandErrored = false
		return nil
	case "quit", "exit":
		m.commandStatus = "Closing Fuse"
		m.commandErrored = false
		return tea.Quit
	default:
		m.commandStatus = "Unknown command: " + head
		m.commandErrored = true
		return nil
	}
}

func (m *model) focusLabel(focused bool) string {
	if focused {
		return "focus"
	}
	if m.width < 110 {
		return "tab to cycle"
	}
	return ""
}

func (m *model) heroHeights(gapLines int) (int, int) {
	available := max(12, m.height-m.chromeHeight()-gapLines)
	hero := clamp(available/3, 9, 14)
	if hero > available-6 {
		hero = available - 6
	}
	body := max(6, available-hero)
	return hero, body
}

func fetchSnapshotCmd(ctx context.Context, cli Client, eventLimit int) tea.Cmd {
	return func() tea.Msg {
		attemptedAt := time.Now().UTC()
		snap, err := collectSnapshot(ctx, cli, eventLimit)
		return snapshotResultMsg{
			snapshot:    snap,
			attemptedAt: attemptedAt,
			err:         err,
		}
	}
}

func waitForContextCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		<-ctx.Done()
		return contextDoneMsg{}
	}
}

func scheduleRefresh(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

func scheduleAnimation() tea.Cmd {
	return tea.Tick(180*time.Millisecond, func(at time.Time) tea.Msg {
		return animationTickMsg{at: at}
	})
}

func cyclePane(current pane, delta int) pane {
	index := 0
	for i, candidate := range paneOrder {
		if candidate == current {
			index = i
			break
		}
	}
	index = (index + delta + len(paneOrder)) % len(paneOrder)
	return paneOrder[index]
}

func splitWidth(total, gap int) (int, int) {
	left := (total - gap) / 2
	right := total - gap - left
	return max(1, left), max(1, right)
}

func splitWidths(total, gap, count int) []int {
	if count <= 1 {
		return []int{max(1, total)}
	}
	usable := total - gap*(count-1)
	base := usable / count
	extra := usable % count
	widths := make([]int, count)
	for i := range widths {
		widths[i] = max(1, base)
		if i < extra {
			widths[i]++
		}
	}
	return widths
}

func barGauge(used, total, width, pulseFrame int, animate bool) string {
	if total <= 0 {
		return strings.Repeat("-", width)
	}
	filled := int(float64(used) / float64(total) * float64(width))
	if filled > width {
		filled = width
	}

	filledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#243244"))
	highlightStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Background(lipgloss.Color("#0EA5E9"))
	highlight := -1
	if animate && filled > 0 {
		highlight = pulseFrame % filled
	}
	parts := make([]string, 0, width+2)
	parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Render("["))
	for i := 0; i < width; i++ {
		switch {
		case i < filled && i == highlight:
			parts = append(parts, highlightStyle.Render("█"))
		case i < filled:
			parts = append(parts, filledStyle.Render("█"))
		default:
			parts = append(parts, emptyStyle.Render("░"))
		}
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Render("]"))
	return strings.Join(parts, "")
}

func stateBadge(state string) string {
	color := lipgloss.Color("#475569")
	switch strings.ToLower(state) {
	case "idle":
		color = lipgloss.Color("#166534")
	case "alloc", "allocated", "mixed":
		color = lipgloss.Color("#0369A1")
	case "down", "drain":
		color = lipgloss.Color("#991B1B")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0")).Background(color).Padding(0, 1).Render(strings.ToUpper(truncateText(state, 10)))
}

func healthBadge(health domain.HealthStatus) string {
	color := lipgloss.Color("#166534")
	switch health {
	case domain.HealthDegraded:
		color = lipgloss.Color("#92400E")
	case domain.HealthOffline:
		color = lipgloss.Color("#991B1B")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Background(color).Padding(0, 1).Render(strings.ToUpper(string(health)))
}

func jobStateBadge(state domain.JobState) string {
	color := lipgloss.Color("#334155")
	switch state {
	case domain.JobStateRunning, domain.JobStateSucceeded:
		color = lipgloss.Color("#166534")
	case domain.JobStatePending, domain.JobStateSubmitting, domain.JobStateRequeued:
		color = lipgloss.Color("#0F766E")
	case domain.JobStateCompleting:
		color = lipgloss.Color("#0369A1")
	case domain.JobStateFailed, domain.JobStateCancelled, domain.JobStateCancelling:
		color = lipgloss.Color("#991B1B")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Background(color).Padding(0, 1).Render(string(state))
}

func reasonBadge(reason domain.ReasonCode) string {
	color := lipgloss.Color("#334155")
	switch reason {
	case domain.ReasonScheduled:
		color = lipgloss.Color("#166534")
	case domain.ReasonInsufficientGPUs, domain.ReasonTopologyUnsatisfied, domain.ReasonQuotaExceeded, domain.ReasonSlurmQueueBacklog:
		color = lipgloss.Color("#92400E")
	case domain.ReasonSubmissionFailed, domain.ReasonUnknown, domain.ReasonCancelledByOperator, domain.ReasonExternalCancellation:
		color = lipgloss.Color("#991B1B")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Background(color).Padding(0, 1).Render(strings.ToUpper(string(reason)))
}

type metricCard struct {
	Label string
	Value string
}

func (m *model) renderTab(target pane, title string, count int) string {
	label := fmt.Sprintf("%s %d", title, count)
	if target == m.selected {
		return m.styles.tabActive.Render(label)
	}
	return m.styles.tabMuted.Render(label)
}

func (m *model) snapshotAgeLabel() string {
	if !m.hasSnapshot || m.data.CapturedAt.IsZero() {
		return "pending"
	}
	now := m.now
	if now.IsZero() {
		now = time.Now()
	}
	delta := now.Sub(m.data.CapturedAt)
	if delta < 0 {
		delta = 0
	}
	if delta < time.Second {
		return "now"
	}
	if delta < time.Minute {
		return fmt.Sprintf("%ds", int(delta.Seconds()))
	}
	if delta < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(delta.Minutes()), int(delta.Seconds())%60)
	}
	return delta.Round(time.Second).String()
}

func (m *model) totalDeviceCapacity() int {
	total := 0
	for _, node := range m.data.Nodes {
		total += max(node.TotalGPUs, node.DeviceCount)
	}
	return total
}

func (m *model) countNodeHealth(health domain.HealthStatus) int {
	count := 0
	for _, node := range m.data.Nodes {
		if node.Health == health {
			count++
		}
	}
	return count
}

func (m *model) hottestNodeLabel() string {
	var hottest nodeSummary
	for _, node := range m.data.Nodes {
		if node.MaxTempC > hottest.MaxTempC {
			hottest = node
		}
	}
	if hottest.MaxTempC == 0 {
		return "--"
	}
	return fmt.Sprintf("%s %dC", hottest.Name, hottest.MaxTempC)
}

func (m *model) averageUtilLabel() string {
	if len(m.data.Nodes) == 0 {
		return "--"
	}
	total := 0
	count := 0
	for _, node := range m.data.Nodes {
		if node.DeviceCount == 0 {
			continue
		}
		total += node.AverageUtil
		count++
	}
	if count == 0 {
		return "--"
	}
	return fmt.Sprintf("%d%%", total/count)
}

func flexLine(width int, left, right string) string {
	space := max(1, width-lipgloss.Width(left)-lipgloss.Width(right)-1)
	return lipgloss.JoinHorizontal(lipgloss.Center, left, strings.Repeat(" ", space), right)
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(value)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > width {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return "…"
	}
	return string(runes) + "…"
}

func truncateStyled(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	plain := ansi.Strip(value)
	return truncateText(plain, width)
}

func fitLines(value string, height int) string {
	if height <= 0 {
		return ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= height {
		return value
	}
	return strings.Join(lines[:height], "\n")
}

func padRight(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func joinHorizontalWithGap(gap int, values ...string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values)*2-1)
	for i, value := range values {
		if i > 0 {
			parts = append(parts, strings.Repeat(" ", gap))
		}
		parts = append(parts, value)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
