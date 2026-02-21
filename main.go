package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewState int

const (
	stateVaultSelect viewState = iota
	stateVaultCreate
	stateVaultOpenPath
	stateFileList
	stateFileCreate
	stateDirCreate
	stateEditor
	stateConfirmDelete
)

type Model struct {
	state    viewState
	list     list.Model
	input    textinput.Model
	textarea textarea.Model
	windowW  int
	windowH  int
	vault    string
	current  string
	editing  string
	lastList viewState
	status   string
	pending  *deleteTarget
}

type vaultRegistry struct {
	Vaults []string `json:"vaults"`
}

type deleteTarget struct {
	path    string
	label   string
	isDir   bool
	isVault bool
}

var errFolderDialogCanceled = errors.New("folder dialog canceled")

var (
	colorPrimary = lipgloss.AdaptiveColor{Light: "#0F4C5C", Dark: "#7AD9F5"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "#475467", Dark: "#D4DEE8"}
	colorBorder  = lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#3B4A5A"}
	colorSuccess = lipgloss.AdaptiveColor{Light: "#1F7A3F", Dark: "#67D08B"}
	colorWarning = lipgloss.AdaptiveColor{Light: "#B54708", Dark: "#FDBA74"}
	colorError   = lipgloss.AdaptiveColor{Light: "#B42318", Dark: "#FF8D8D"}

	appStyle        = lipgloss.NewStyle()
	panelStyle      = lipgloss.NewStyle().Padding(0, 1)
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	subtitleStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	hintStyle       = lipgloss.NewStyle().Foreground(colorMuted)
	statusInfoStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	statusOkStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
	statusWarnStyle = lipgloss.NewStyle().Bold(true).Foreground(colorWarning)
	statusErrStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorError)
)

func initialModel() Model {
	items := getVaults()
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Foreground(colorPrimary)
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.Foreground(colorMuted)
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Bold(true).Foreground(colorSuccess)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(colorSuccess)
	delegate.Styles.DimmedTitle = delegate.Styles.DimmedTitle.Foreground(colorMuted)
	delegate.Styles.DimmedDesc = delegate.Styles.DimmedDesc.Foreground(colorMuted)
	delegate.SetSpacing(0)

	l := list.New(items, delegate, 0, 0)
	l.Title = "Select vault (Enter), create (Ctrl+N), open by path (Ctrl+O), open in explorer (Ctrl+P)"
	listStyles := list.DefaultStyles()
	listStyles.Title = listStyles.Title.Bold(true).Foreground(colorPrimary)
	listStyles.TitleBar = listStyles.TitleBar.BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(colorBorder)
	listStyles.PaginationStyle = listStyles.PaginationStyle.Foreground(colorMuted)
	listStyles.HelpStyle = listStyles.HelpStyle.Foreground(colorMuted)
	listStyles.StatusBar = listStyles.StatusBar.Foreground(colorMuted)
	listStyles.StatusEmpty = listStyles.StatusEmpty.Foreground(colorMuted)
	listStyles.FilterPrompt = listStyles.FilterPrompt.Foreground(colorPrimary).Bold(true)
	listStyles.FilterCursor = listStyles.FilterCursor.Foreground(colorPrimary)
	l.Styles = listStyles
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)

	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = 200
	in.Width = 60
	in.PromptStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	in.TextStyle = lipgloss.NewStyle().Foreground(colorPrimary)
	in.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorMuted)
	in.Cursor.Style = lipgloss.NewStyle().Foreground(colorPrimary)

	ta := textarea.New()
	ta.Prompt = "> "
	ta.ShowLineNumbers = true
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	ta.FocusedStyle.LineNumber = lipgloss.NewStyle().Foreground(colorMuted)
	ta.FocusedStyle.CursorLineNumber = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle().Foreground(colorPrimary)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colorMuted)
	ta.BlurredStyle = ta.FocusedStyle

	return Model{
		state:    stateVaultSelect,
		list:     l,
		input:    in,
		textarea: ta,
		windowW:  80,
		windowH:  24,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			switch m.state {
			case stateEditor:
				m.state = stateFileList
				m.textarea.Blur()
				m = m.refreshFileList()
				return m, nil
			case stateVaultCreate, stateVaultOpenPath, stateFileCreate, stateDirCreate, stateConfirmDelete:
				m.state = m.lastList
				m.input.Blur()
				m.pending = nil
				return m, nil
			}
		case "n":
			if m.state == stateConfirmDelete {
				m.state = m.lastList
				m.pending = nil
				return m, nil
			}
		case "y":
			if m.state == stateConfirmDelete {
				return m.confirmDelete()
			}
		case "ctrl+s":
			if m.state == stateEditor {
				err := os.WriteFile(m.editing, []byte(m.textarea.Value()), 0644)
				if err != nil {
					m.status = "Error: " + err.Error()
				} else {
					m.status = "Saved: " + relOrBase(m.vault, m.editing)
				}
				return m, nil
			}
		case "ctrl+n":
			switch m.state {
			case stateVaultSelect:
				m = m.enterPrompt(stateVaultCreate, "New vault name")
				return m, textinput.Blink
			case stateFileList:
				m = m.enterPrompt(stateFileCreate, "File name: letters and digits only")
				return m, textinput.Blink
			}
		case "ctrl+o":
			if m.state == stateVaultSelect {
				m = m.enterPrompt(stateVaultOpenPath, "Vault path (absolute or relative)")
				return m, textinput.Blink
			}
		case "ctrl+p":
			if m.state == stateVaultSelect {
				return m.openVaultByExplorer()
			}
		case "ctrl+d":
			if m.state == stateFileList {
				m = m.enterPrompt(stateDirCreate, "New directory name (in current directory)")
				return m, textinput.Blink
			}
		case "ctrl+x":
			switch m.state {
			case stateVaultSelect:
				selected := m.list.SelectedItem()
				if selected == nil {
					return m, nil
				}
				it := selected.(item)
				if it.mode != "" {
					return m, nil
				}
				m.pending = &deleteTarget{
					path:    it.path,
					label:   filepath.Base(it.path),
					isDir:   true,
					isVault: true,
				}
				m.lastList = stateVaultSelect
				m.state = stateConfirmDelete
				return m, nil
			case stateFileList:
				selected := m.list.SelectedItem()
				if selected == nil {
					return m, nil
				}
				it := selected.(item)
				if it.mode == "up" {
					return m, nil
				}
				m.pending = &deleteTarget{
					path:  it.path,
					label: relOrBase(m.vault, it.path),
					isDir: it.isDir,
				}
				m.lastList = stateFileList
				m.state = stateConfirmDelete
				return m, nil
			}
		case "backspace":
			if m.state == stateFileList {
				m = m.goParent()
				return m, nil
			}
		case "enter":
			if m.state == stateConfirmDelete {
				return m.confirmDelete()
			}
			return m.handleEnter()
		}
	case tea.WindowSizeMsg:
		m.windowW = msg.Width
		m.windowH = msg.Height
		m = m.applyResponsiveLayout()
	}

	m = m.applyResponsiveLayout()

	switch m.state {
	case stateVaultSelect, stateFileList:
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	case stateEditor:
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	case stateVaultCreate, stateVaultOpenPath, stateFileCreate, stateDirCreate:
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.state {
	case stateVaultSelect:
		selected := m.list.SelectedItem()
		if selected == nil {
			return m, nil
		}
		it := selected.(item)
		if it.mode == "create-vault" {
			m = m.enterPrompt(stateVaultCreate, "New vault name")
			return m, textinput.Blink
		}
		if it.mode == "open-vault-path" {
			m = m.enterPrompt(stateVaultOpenPath, "Vault path (absolute or relative)")
			return m, textinput.Blink
		}
		if it.mode == "open-vault-explorer" {
			return m.openVaultByExplorer()
		}
		m.vault = it.path
		m.current = it.path
		m.state = stateFileList
		m.status = "Vault selected: " + filepath.Base(it.path)
		m = m.refreshFileList()
		return m, nil
	case stateFileList:
		selected := m.list.SelectedItem()
		if selected == nil {
			return m, nil
		}
		it := selected.(item)
		if it.mode == "up" {
			m = m.goParent()
			return m, nil
		}
		if it.isDir {
			m.current = it.path
			m = m.refreshFileList()
			return m, nil
		}
		content, err := os.ReadFile(it.path)
		if err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		m.editing = it.path
		m.textarea.SetValue(string(content))
		m.textarea.Focus()
		m.state = stateEditor
		return m, textarea.Blink
	case stateVaultCreate:
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			m.status = "Vault name cannot be empty"
			return m, nil
		}
		path := filepath.Join(vaultStorageRoot(), name)
		if err := os.Mkdir(path, 0755); err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		if err := registerVault(abs); err != nil {
			m.status = "Vault created, but registry update failed: " + err.Error()
			return m, nil
		}
		m.vault = abs
		m.current = abs
		m.state = stateFileList
		m.status = "Vault created: " + filepath.Base(abs)
		m = m.refreshFileList()
		return m, nil
	case stateVaultOpenPath:
		return m.openVaultPath(m.input.Value())
	case stateFileCreate:
		baseName := strings.TrimSpace(m.input.Value())
		if baseName == "" {
			m.status = "File name cannot be empty"
			return m, nil
		}
		if !isAlnumName(baseName) {
			m.status = "Invalid file name: use only letters and digits"
			return m, nil
		}
		name := baseName + ".md"
		path, err := m.safePath(name)
		if err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		_ = file.Close()
		m.state = stateFileList
		m.status = "File created: " + relOrBase(m.vault, path)
		m = m.refreshFileList()
		return m, nil
	case stateDirCreate:
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			m.status = "Directory name cannot be empty"
			return m, nil
		}
		path, err := m.safePath(name)
		if err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			m.status = "Error: " + err.Error()
			return m, nil
		}
		m.state = stateFileList
		m.status = "Directory created: " + relOrBase(m.vault, path)
		m = m.refreshFileList()
		return m, nil
	default:
		return m, nil
	}
}

func (m Model) View() string {
	m = m.applyResponsiveLayout()
	contentW, _ := m.contentDims()

	switch m.state {
	case stateVaultSelect:
		return renderScreen(
			contentW,
			"Vaults",
			"Storage: "+shrinkText(vaultStorageRoot(), maxInt(24, contentW-10)),
			m.list.View(),
			vaultSelectHints(contentW),
			m.status,
		)
	case stateFileList:
		return renderScreen(
			contentW,
			"Vault: "+filepath.Base(m.vault),
			"Path: "+shrinkText(relOrDot(m.vault, m.current), maxInt(24, contentW-7)),
			m.list.View(),
			fileListHints(contentW),
			m.status,
		)
	case stateEditor:
		return renderScreen(
			contentW,
			"Editing: "+relOrBase(m.vault, m.editing),
			"Markdown editor",
			m.textarea.View(),
			"Ctrl+S: save | Esc: back",
			m.status,
		)
	case stateVaultCreate:
		return renderScreen(
			contentW,
			"Create Vault",
			"Enter name and press Enter",
			m.input.View(),
			"Esc: cancel",
			m.status,
		)
	case stateVaultOpenPath:
		return renderScreen(
			contentW,
			"Open Vault By Path",
			"Enter full or relative folder path",
			m.input.View(),
			"Esc: cancel",
			m.status,
		)
	case stateFileCreate:
		return renderScreen(
			contentW,
			"Create File",
			"Use only letters and digits, .md is added automatically",
			m.input.View(),
			"Esc: cancel",
			m.status,
		)
	case stateDirCreate:
		return renderScreen(
			contentW,
			"Create Directory",
			"Enter a directory name",
			m.input.View(),
			"Esc: cancel",
			m.status,
		)
	case stateConfirmDelete:
		if m.pending == nil {
			return renderScreen(
				contentW,
				"Delete",
				"Nothing selected for deletion",
				"",
				"Esc: back",
				m.status,
			)
		}
		target := "file"
		if m.pending.isDir {
			target = "directory"
		}
		if m.pending.isVault {
			target = "vault"
		}
		return renderScreen(
			contentW,
			"Delete "+target+"?",
			"",
			m.pending.label,
			deleteHints(contentW),
			m.status,
		)
	default:
		return ""
	}
}

func renderScreen(contentW int, title string, subtitle string, body string, hints string, status string) string {
	if contentW < 20 {
		contentW = 20
	}
	parts := make([]string, 0, 5)
	if strings.TrimSpace(title) != "" {
		parts = append(parts, titleStyle.MaxWidth(contentW).Render(title))
	}
	if strings.TrimSpace(subtitle) != "" {
		parts = append(parts, subtitleStyle.MaxWidth(contentW).Render(subtitle))
	}
	if strings.TrimSpace(status) != "" {
		parts = append(parts, renderStatus(status, contentW))
	}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, body)
	}
	if strings.TrimSpace(hints) != "" {
		parts = append(parts, hintStyle.MaxWidth(contentW).Render(hints))
	}
	content := strings.Join(parts, "\n")
	return appStyle.Render(panelStyle.Render(content))
}

func renderStatus(status string, contentW int) string {
	s := strings.TrimSpace(status)
	if s == "" {
		return ""
	}
	if contentW < 20 {
		contentW = 20
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(s, "Error:"):
		return statusErrStyle.MaxWidth(contentW).Render(s)
	case strings.Contains(lower, "deleted"):
		return statusWarnStyle.MaxWidth(contentW).Render(s)
	case strings.Contains(lower, "created"), strings.Contains(lower, "saved"), strings.Contains(lower, "selected"):
		return statusOkStyle.MaxWidth(contentW).Render(s)
	default:
		return statusInfoStyle.MaxWidth(contentW).Render(s)
	}
}

type item struct {
	title string
	desc  string
	path  string
	isDir bool
	mode  string
}

func (i item) Title() string {
	return i.title
}

func (i item) Description() string {
	return i.desc
}

func (i item) FilterValue() string {
	return i.title
}

func getVaults() []list.Item {
	paths, err := loadVaultRegistry()
	if err != nil {
		paths = []string{}
	}

	var dirs []item
	var validPaths []string
	for _, p := range paths {
		abs, absErr := filepath.Abs(p)
		if absErr != nil {
			continue
		}
		info, statErr := os.Stat(abs)
		if statErr != nil || !info.IsDir() {
			continue
		}
		validPaths = append(validPaths, abs)
		dirs = append(dirs, item{
			title: filepath.Base(abs),
			desc:  "Created vault",
			path:  abs,
			isDir: true,
		})
	}
	_ = saveVaultRegistry(validPaths)

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].title) < strings.ToLower(dirs[j].title)
	})

	items := make([]list.Item, 0, len(dirs)+3)
	for _, d := range dirs {
		items = append(items, d)
	}
	items = append(items, item{
		title: "+ Create new vault",
		desc:  "Create a new directory and open it as vault",
		mode:  "create-vault",
	})
	items = append(items, item{
		title: "+ Open vault by path",
		desc:  "Open any existing directory as vault",
		mode:  "open-vault-path",
	})
	items = append(items, item{
		title: "+ Open vault in explorer",
		desc:  "Pick an existing directory in a folder dialog",
		mode:  "open-vault-explorer",
	})
	return items
}

func (m Model) refreshFileList() Model {
	files, err := os.ReadDir(m.current)
	if err != nil {
		m.status = "Error: " + err.Error()
		return m
	}

	entries := make([]item, 0, len(files))
	for _, file := range files {
		p := filepath.Join(m.current, file.Name())
		entry := item{
			title: file.Name(),
			desc:  "",
			path:  p,
			isDir: file.IsDir(),
		}
		if file.IsDir() {
			entry.title = file.Name() + string(os.PathSeparator)
			entry.desc = "Directory"
		} else {
			if info, infoErr := file.Info(); infoErr == nil {
				entry.desc = "Modified: " + info.ModTime().Format("02 Jan 15:04")
			}
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return strings.ToLower(entries[i].title) < strings.ToLower(entries[j].title)
	})

	items := make([]list.Item, 0, len(entries)+1)
	if !samePath(m.current, m.vault) {
		items = append(items, item{
			title: "..",
			desc:  "Go to parent directory",
			path:  filepath.Dir(m.current),
			isDir: true,
			mode:  "up",
		})
	}
	for _, e := range entries {
		items = append(items, e)
	}

	m.list.SetItems(items)
	m.list.Title = "Vault explorer"
	return m
}

func (m Model) enterPrompt(state viewState, placeholder string) Model {
	m.lastList = m.state
	m.state = state
	m.input.SetValue("")
	m.input.Placeholder = placeholder
	m.input.Focus()
	return m
}

func (m Model) openVaultPath(rawPath string) (tea.Model, tea.Cmd) {
	cleanPath := strings.Trim(strings.TrimSpace(rawPath), "\"'")
	if cleanPath == "" {
		m.status = "Vault path cannot be empty"
		return m, nil
	}
	abs, err := filepath.Abs(cleanPath)
	if err != nil {
		m.status = "Error: " + err.Error()
		return m, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		m.status = "Error: cannot access this path"
		return m, nil
	}
	if !info.IsDir() {
		m.status = "Error: path must point to a directory"
		return m, nil
	}

	m.vault = abs
	m.current = abs
	m.state = stateFileList
	m.status = "Vault selected: " + filepath.Base(abs)
	m = m.refreshFileList()
	return m, nil
}

func (m Model) openVaultByExplorer() (tea.Model, tea.Cmd) {
	path, err := pickFolderInExplorer()
	if err != nil {
		if errors.Is(err, errFolderDialogCanceled) {
			m.status = "Vault selection canceled"
			return m, nil
		}
		m.status = "Error: " + err.Error()
		return m, nil
	}
	return m.openVaultPath(path)
}

func (m Model) applyResponsiveLayout() Model {
	contentW, contentH := m.contentDims()

	m.input.Width = inputWidth(contentW)

	reserved := 0
	switch m.state {
	case stateVaultSelect:
		reserved = reserved + 1 + 1 + wrappedLineCount(vaultSelectHints(contentW), contentW)
	case stateFileList:
		reserved = reserved + 1 + 1 + wrappedLineCount(fileListHints(contentW), contentW)
	case stateEditor:
		reserved = reserved + 1 + 1 + wrappedLineCount("Ctrl+S: save | Esc: back", contentW)
	case stateVaultCreate:
		reserved = reserved + 1 + 1 + wrappedLineCount("Esc: cancel", contentW)
	case stateVaultOpenPath:
		reserved = reserved + 1 + 1 + wrappedLineCount("Esc: cancel", contentW)
	case stateFileCreate:
		reserved = reserved + 1 + 1 + wrappedLineCount("Esc: cancel", contentW)
	case stateDirCreate:
		reserved = reserved + 1 + 1 + wrappedLineCount("Esc: cancel", contentW)
	case stateConfirmDelete:
		reserved = reserved + 1 + wrappedLineCount(deleteHints(contentW), contentW)
	}
	if strings.TrimSpace(m.status) != "" {
		reserved = reserved + wrappedLineCount(m.status, contentW)
	}
	reserved = reserved + 2

	bodyH := maxInt(4, contentH-reserved)

	m.list.SetSize(contentW, bodyH)
	m.textarea.SetWidth(contentW)
	m.textarea.SetHeight(maxInt(5, bodyH))
	return m
}

func (m Model) contentDims() (int, int) {
	windowW := m.windowW
	windowH := m.windowH
	if windowW <= 0 || windowH <= 0 {
		windowW = 80
		windowH = 24
	}
	return contentSize(windowW, windowH)
}

func (m Model) goParent() Model {
	if samePath(m.current, m.vault) {
		return m
	}
	parent := filepath.Dir(m.current)
	if insideVault(m.vault, parent) {
		m.current = parent
		m = m.refreshFileList()
	}
	return m
}

func (m Model) safePath(name string) (string, error) {
	target := filepath.Join(m.current, name)
	if !insideVault(m.vault, target) {
		return "", fmt.Errorf("path escapes vault")
	}
	return target, nil
}

func insideVault(vault string, p string) bool {
	absVault, err := filepath.Abs(vault)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absVault, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func samePath(a string, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	return aa == bb
}

func relOrDot(base string, p string) string {
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == "." {
		return "."
	}
	return rel
}

func relOrBase(base string, p string) string {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return filepath.Base(p)
	}
	return rel
}

func vaultStorageRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "."
	}
	return home
}

func vaultRegistryPath() string {
	return filepath.Join(vaultStorageRoot(), ".gono_vaults.json")
}

func loadVaultRegistry() ([]string, error) {
	data, err := os.ReadFile(vaultRegistryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var reg vaultRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	out := make([]string, 0, len(reg.Vaults))
	for _, v := range reg.Vaults {
		clean := strings.TrimSpace(v)
		if clean == "" {
			continue
		}
		abs, absErr := filepath.Abs(clean)
		if absErr != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out, nil
}

func saveVaultRegistry(vaults []string) error {
	seen := make(map[string]struct{})
	clean := make([]string, 0, len(vaults))
	for _, v := range vaults {
		p := strings.TrimSpace(v)
		if p == "" {
			continue
		}
		abs, absErr := filepath.Abs(p)
		if absErr != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		clean = append(clean, abs)
	}
	sort.Slice(clean, func(i, j int) bool {
		return strings.ToLower(clean[i]) < strings.ToLower(clean[j])
	})

	reg := vaultRegistry{Vaults: clean}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(vaultRegistryPath(), data, 0644)
}

func registerVault(path string) error {
	vaults, err := loadVaultRegistry()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, v := range vaults {
		if samePath(v, abs) {
			return nil
		}
	}
	vaults = append(vaults, abs)
	return saveVaultRegistry(vaults)
}

func unregisterVault(path string) error {
	vaults, err := loadVaultRegistry()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	filtered := make([]string, 0, len(vaults))
	for _, v := range vaults {
		if samePath(v, abs) {
			continue
		}
		filtered = append(filtered, v)
	}
	return saveVaultRegistry(filtered)
}

func (m Model) confirmDelete() (tea.Model, tea.Cmd) {
	if m.pending == nil {
		m.state = m.lastList
		return m, nil
	}

	target := *m.pending
	var err error
	if target.isDir {
		err = os.RemoveAll(target.path)
	} else {
		err = os.Remove(target.path)
	}
	if err != nil {
		m.status = "Error: " + err.Error()
		m.pending = nil
		m.state = m.lastList
		return m, nil
	}

	if target.isVault {
		if regErr := unregisterVault(target.path); regErr != nil {
			m.status = "Vault deleted, but registry update failed: " + regErr.Error()
		} else {
			m.status = "Vault deleted: " + target.label
		}
		m.list.SetItems(getVaults())
		m.list.Title = "Select vault (Enter), create (Ctrl+N), open by path (Ctrl+O), open in explorer (Ctrl+P)"
	} else {
		m.status = "Deleted: " + target.label
		m = m.refreshFileList()
	}

	m.pending = nil
	m.state = m.lastList
	return m, nil
}

func pickFolderInExplorer() (string, error) {
	switch runtime.GOOS {
	case "windows":
		script := "[void][Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms');" +
			"$dialog=New-Object System.Windows.Forms.FolderBrowserDialog;" +
			"$dialog.Description='Select vault folder';" +
			"$dialog.ShowNewFolderButton=$true;" +
			"if($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){[Console]::Out.Write($dialog.SelectedPath)}"
		out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
		if err != nil {
			return "", fmt.Errorf("cannot open folder picker: %w", err)
		}
		p := strings.TrimSpace(string(out))
		if p == "" {
			return "", errFolderDialogCanceled
		}
		return p, nil
	default:
		return "", fmt.Errorf("folder picker is not implemented for %s", runtime.GOOS)
	}
}

func isAlnumName(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func vaultSelectHints(width int) string {
	if width < 72 {
		return "Ctrl+N create | Ctrl+O path\nCtrl+P explorer | Ctrl+X delete"
	}
	return "Ctrl+N: create vault | Ctrl+O: open by path | Ctrl+P: open in explorer | Ctrl+X: delete vault"
}

func fileListHints(width int) string {
	if width < 72 {
		return "Enter open | Backspace up | Ctrl+N file\nCtrl+D dir | Ctrl+X delete | Ctrl+C quit"
	}
	return "Enter: open | Backspace: up | Ctrl+N: new file | Ctrl+D: new dir | Ctrl+X: delete | Ctrl+C: quit"
}

func deleteHints(width int) string {
	if width < 58 {
		return "Y/Enter: delete\nN/Esc: cancel"
	}
	return "Y/Enter: delete permanently | N/Esc: cancel"
}

func shrinkText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

func wrappedLineCount(s string, width int) int {
	if width <= 0 || strings.TrimSpace(s) == "" {
		return 0
	}
	lines := 0
	for _, line := range strings.Split(s, "\n") {
		r := len([]rune(line))
		if r == 0 {
			lines++
			continue
		}
		lines += (r-1)/width + 1
	}
	return lines
}

func contentSize(windowW int, windowH int) (int, int) {
	appW, appH := appStyle.GetFrameSize()
	panelW, panelH := panelStyle.GetFrameSize()

	width := windowW - appW - panelW
	height := windowH - appH - panelH

	if width < 20 {
		width = 20
	}
	if height < 8 {
		height = 8
	}
	return width, height
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func inputWidth(window int) int {
	if window <= 20 {
		return 16
	}
	if window-4 < 60 {
		return window - 4
	}
	return 60
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
