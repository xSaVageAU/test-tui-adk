package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// rootAgentMenuID is the paletteItem id standing in for AgentConfigSummary's
// root entry (whose real ID is "") throughout /agents' menu tree —
// paletteItem.id can't distinguish "" (root) from "no selection", so this
// package-reserved string is used in the UI layer instead and translated
// back via agentConfigID right before calling setAgentProvider/setAgentModel.
const rootAgentMenuID = "root"

// defaultModelName mirrors adk.DefaultModelName's exact value — this
// package doesn't import internal/adk (see backend.go), so it's
// duplicated purely for display: showing what an unset Gemini model
// field will actually resolve to, without pretending to know what any
// other provider's default might be.
const defaultModelName = "gemini-3.1-flash-lite"

// openAgentsMenu is /agents' entry point: root plus every discovered
// sub-agent, one row each. Snapshots the list into agentMenuSummaries so
// the detail/provider/model steps that follow don't need their own
// round trip just to show what's currently set.
func (a *App) openAgentsMenu() {
	if a.listAgents == nil {
		a.systemMessage("Agent configuration isn't available.")
		return
	}
	agents, err := a.listAgents()
	if err != nil {
		a.systemMessage("Could not read agent configs: " + err.Error())
		return
	}
	a.agentMenuSummaries = agents

	items := make([]paletteItem, len(agents))
	for i, ag := range agents {
		id, role := ag.ID, "sub-agent"
		if ag.IsRoot {
			id, role = rootAgentMenuID, "root"
		}
		items[i] = paletteItem{id: id, title: ag.Name, desc: role + " — " + displayProvider(ag.Provider)}
	}
	a.openMenu(paletteAgents, "Configure agents", items)
}

// openAgentDetailMenu is /agents' second step: Provider/Model for
// whichever agent menuID names.
func (a *App) openAgentDetailMenu(menuID string) {
	ag, ok := a.agentSummaryFor(menuID)
	if !ok {
		a.systemMessage("That agent is no longer available.")
		return
	}
	a.agentMenuTarget = menuID
	items := []paletteItem{
		{id: "provider", title: "Provider", desc: displayProvider(ag.Provider) + " — select to change"},
		{id: "model", title: "Model", desc: displayModel(ag.Provider, ag.Model) + " — select to edit"},
	}
	a.openMenu(paletteAgentDetail, ag.Name, items)
}

// confirmAgentDetailSelection routes the Provider/Model choice to
// whichever popup handles it — both are terminal from handlePaletteKey's
// point of view once they themselves open (paletteAgentProvider is a
// list, the model field is paletteTextInput), so this always returns
// false ("don't close yet", the new popup is already showing).
func (a *App) confirmAgentDetailSelection(id string) (bool, tea.Cmd) {
	switch id {
	case "provider":
		a.openAgentProviderMenu()
	case "model":
		a.openAgentModelInput()
	}
	return false, nil
}

func (a *App) openAgentProviderMenu() {
	items := []paletteItem{
		{id: providerGemini, title: "Gemini", desc: "Google's Gemini API"},
		{id: providerOpenRouter, title: "OpenRouter", desc: "OpenAI-compatible, many models"},
	}
	a.openMenu(paletteAgentProvider, "Choose provider", items)
}

// openAgentModelInput shows the unmasked model-slug popup for whichever
// agent agentMenuTarget names, prefilled with its current value so
// editing doesn't require retyping an unrelated part of the slug.
func (a *App) openAgentModelInput() {
	ag, ok := a.agentSummaryFor(a.agentMenuTarget)
	if !ok {
		a.systemMessage("That agent is no longer available.")
		return
	}

	ti := textinput.New()
	ti.Placeholder = modelPlaceholderFor(ag.Provider)
	ti.CharLimit = 256
	ti.Width = a.textPopupWidth() - 4
	ti.PromptStyle = a.styles.InputPrompt
	ti.PlaceholderStyle = a.styles.InputHint
	ti.SetValue(ag.Model)
	ti.CursorEnd()
	ti.Focus()

	a.keyInput = ti
	a.textPopupKind = textPopupAgentModel
	a.textPopupLabel = "Set model for " + ag.Name
	a.paletteKind = paletteTextInput
}

// saveAgentProvider persists provider for agentMenuTarget and kicks off
// a reload — called from confirmMenuSelection's paletteAgentProvider
// case, so id there is the chosen provider, not an agent id.
func (a *App) saveAgentProvider(provider string) tea.Cmd {
	if a.setAgentProvider == nil {
		return nil
	}
	if err := a.setAgentProvider(agentConfigID(a.agentMenuTarget), provider); err != nil {
		a.systemMessage("Could not update provider: " + err.Error())
		return nil
	}
	a.systemMessage("Provider set to " + providerDisplayName(provider) + ". Reloading agents...")
	return a.reloadBackend()
}

// submitAgentModel is paletteTextInput's Enter handler when
// textPopupKind is textPopupAgentModel — persists the typed model slug
// for agentMenuTarget and kicks off a reload, same as saveAgentProvider.
func (a *App) submitAgentModel() tea.Cmd {
	modelName := strings.TrimSpace(a.keyInput.Value())
	target := a.agentMenuTarget
	a.closeMenu()

	if a.setAgentModel == nil {
		return nil
	}
	if err := a.setAgentModel(agentConfigID(target), modelName); err != nil {
		a.systemMessage("Could not update model: " + err.Error())
		return nil
	}
	a.systemMessage("Model updated. Reloading agents...")
	return a.reloadBackend()
}

// reloadBackend rebuilds the whole backend from whatever's currently
// saved on disk — no fresh key of its own — so a /agents edit takes
// effect immediately instead of requiring a restart. Shares
// newBackend/keySetMsg with /key: passing "", "" tells adk.New to
// resolve every agent's key from its own configured provider's saved
// credentials (see main.go's newBackend — an empty apiKey there means
// it skips re-saving, so this never touches credentials.json).
func (a *App) reloadBackend() tea.Cmd {
	if a.newBackend == nil {
		return nil
	}
	if a.turnInProgress() {
		a.systemMessage("Saved — reload (restart, or edit again) once the current response finishes for it to take effect.")
		return nil
	}
	factory := a.newBackend
	return func() tea.Msg {
		backend, err := factory(context.Background(), "", "")
		return keySetMsg{backend: backend, err: err, successMsg: "Agents reloaded.", failPrefix: "Config saved, but could not reload"}
	}
}

// agentSummaryFor looks up agentMenuSummaries by menu id (rootAgentMenuID
// or a sub-agent's own ID) — false if it's gone missing since the list
// was snapshotted (a directory deleted mid-flow, an unlikely but cheap
// case to guard rather than ignore).
func (a *App) agentSummaryFor(menuID string) (AgentConfigSummary, bool) {
	for _, ag := range a.agentMenuSummaries {
		if ag.IsRoot && menuID == rootAgentMenuID {
			return ag, true
		}
		if !ag.IsRoot && ag.ID == menuID {
			return ag, true
		}
	}
	return AgentConfigSummary{}, false
}

// agentConfigID is the inverse of how openAgentsMenu built each row's
// id: rootAgentMenuID back to "" (what SetAgentProvider/SetAgentModel
// expect for root), anything else passed through unchanged.
func agentConfigID(menuID string) string {
	if menuID == rootAgentMenuID {
		return ""
	}
	return menuID
}

func displayProvider(provider string) string {
	if provider == "" {
		return providerDisplayName(providerGemini) + " (default)"
	}
	return providerDisplayName(provider)
}

func displayModel(provider, modelName string) string {
	if modelName != "" {
		return modelName
	}
	if provider == "" || provider == providerGemini {
		return defaultModelName + " (default)"
	}
	return "(not set)"
}

func modelPlaceholderFor(provider string) string {
	if provider == providerOpenRouter {
		return "e.g. openai/gpt-4o-mini"
	}
	return "e.g. " + defaultModelName
}
