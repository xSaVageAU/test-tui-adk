package adk

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
)

const (
	// AgentName is the root agent's name — the only agent that ever
	// speaks; session.Event.Author is always this, since the specialists
	// below are consulted via agent-as-tool (agenttool.New), not transfer
	// (agent.Config.SubAgents) — the root calls them like a function and
	// stays in control, rather than handing the conversation off to a
	// different visible identity. Exported so callers (the boot banner's
	// initial "agent" line, via ui.AppConfig.AgentName) can display it
	// without duplicating the string.
	AgentName = "assistant"

	researchAgentName = "research"
	coderAgentName    = "coder"
	plannerAgentName  = "planner"

	rootInstruction = "You are the front-line assistant embedded in a terminal chat UI test harness. " +
		"Keep replies short — this is a test harness for the UI, not a place for long essays. " +
		"You have a list_files tool for browsing the working directory; use it whenever it's relevant. " +
		"You can consult three specialists via tool calls when a request clearly fits their focus: research " +
		"(reading code/docs, answering questions), coder (writing/editing code), and planner (breaking work " +
		"into steps). Incorporate what they tell you into your own reply — you're still the one answering " +
		"the user. Handle general requests yourself rather than consulting a specialist unnecessarily."

	researchInstruction = "You are the research specialist in a terminal chat UI test harness: read code " +
		"and docs, answer questions accurately and concisely. Use list_files to browse the working " +
		"directory when relevant. Keep replies short."

	coderInstruction = "You are the coding specialist in a terminal chat UI test harness: help write and " +
		"edit code, explain snippets, suggest fixes. Use list_files to browse the working directory when " +
		"relevant. Keep replies short."

	plannerInstruction = "You are the planning specialist in a terminal chat UI test harness: break " +
		"requests down into a clear, ordered list of steps. Keep replies short."
)

// buildRootAgent assembles the whole agent tree: the root "assistant"
// agent plus its three specialists (research, coder, planner), each
// wrapped via agenttool.New so the root consults them like function
// calls rather than transferring the conversation to them — see
// AgentName's doc comment for why. listFilesTool is shared verbatim
// across every agent that wants it (built once by the caller, since
// functiontool.New's result isn't agent-specific state).
func buildRootAgent(m model.LLM, listFilesTool tool.Tool) (agent.Agent, error) {
	research, err := llmagent.New(llmagent.Config{
		Name:        researchAgentName,
		Model:       m,
		Description: "Reads code and docs, answers questions.",
		Instruction: researchInstruction,
		Tools:       []tool.Tool{listFilesTool},
	})
	if err != nil {
		return nil, fmt.Errorf("create research agent: %w", err)
	}

	coder, err := llmagent.New(llmagent.Config{
		Name:        coderAgentName,
		Model:       m,
		Description: "Writes and edits code.",
		Instruction: coderInstruction,
		Tools:       []tool.Tool{listFilesTool},
	})
	if err != nil {
		return nil, fmt.Errorf("create coder agent: %w", err)
	}

	planner, err := llmagent.New(llmagent.Config{
		Name:        plannerAgentName,
		Model:       m,
		Description: "Breaks work into steps.",
		Instruction: plannerInstruction,
	})
	if err != nil {
		return nil, fmt.Errorf("create planner agent: %w", err)
	}

	root, err := llmagent.New(llmagent.Config{
		Name:        AgentName,
		Model:       m,
		Description: "A general-purpose assistant for testing the TUI against a real LLM.",
		Instruction: rootInstruction,
		// agenttool.New wraps each specialist as an ordinary callable
		// tool: the root calls it, gets a result back, and stays the one
		// answering — no SubAgents, no transfer, no change in who's
		// "speaking." nil Config leaves SkipSummarization false, so the
		// root gets a chance to fold each specialist's answer into its
		// own words rather than just relaying it verbatim.
		Tools: []tool.Tool{
			listFilesTool,
			agenttool.New(research, nil),
			agenttool.New(coder, nil),
			agenttool.New(planner, nil),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	return root, nil
}
