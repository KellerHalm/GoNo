package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type status int

const (
	statusListView status = iota
	statusEditorView
)

type Model struct {
	state    status
	list     list.Model
	textarea textarea.Model
	current  string
	err      error
}

func initialModel() Model {

	items := getFiles()

	delegate := list.NewDefaultDelegate()

	return Model{
		state:    statusListView,
		list:     list.New(items, delegate, 0, 0),
		textarea: textarea.New(),
	}

}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
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
			if m.state == statusEditorView {
				m.state = statusListView
				return m, nil
			}
		case "ctrl+s":
			if m.state == statusEditorView {
				err := os.WriteFile(m.current, []byte(m.textarea.Value()), 0644)
				if err != nil {
					m.err = err
				}

				return m, nil
			}
		case "enter":
			if m.state == statusListView {
				selectedItem := m.list.SelectedItem()
				if selectedItem == nil {
					return m, nil
				}

				filename := selectedItem.(item).Title()
				m.current = filename

				content, err := os.ReadFile(filename)
				if err != nil {
					m.err = err
					return m, nil
				}

				m.textarea.SetValue(string(content))

				m.state = statusEditorView
				m.textarea.Focus()

				return m, textarea.Blink

			}
		}

	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		m.textarea.SetWidth(msg.Width)
		m.textarea.SetHeight(msg.Height)

	}

	if m.state == statusListView {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)

}

func (m Model) View() string {
	if m.state == statusListView {
		return m.list.View()
	}

	return m.textarea.View()
}

type item struct {
	title,
	desc string
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

func getFiles() []list.Item {

	files, err := os.ReadDir(".")
	if err != nil {
		return []list.Item{}
	}

	var items []list.Item
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".md") {
			info, _ := file.Info()
			items = append(items, item{
				title: file.Name(),
				desc:  "Modified: " + info.ModTime().Format("02 Jan 15:04"),
			})
		}
	}

	return items
}

func main() {

	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error: ", err)
		os.Exit(1)
	}

}
