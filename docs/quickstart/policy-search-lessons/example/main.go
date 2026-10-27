// This example builds the policy index once and gives each question its own context.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/common"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/models"
	"github.com/rickchristie/gent/policy"
	"github.com/rickchristie/gent/search"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
)

// SamplePolicies contains invented rules for this lesson. Replace them with your own rules.
func SamplePolicies() []*policy.Policy {
	return []*policy.Policy{
		{
			Id:          "return-window",
			Description: "Return unused items within 30 days of delivery.",
			FullContent: `## Returns

An unused item can be returned within 30 days of delivery.
The customer must supply the order ID. Check the delivery date and item condition.
The refund uses the original payment method after the return is checked.`,
			Keywords:         []string{"return", "refund", "unused", "30 days"},
			SyntheticQueries: []string{"I changed my mind about an item I bought"},
		},
		{
			Id:          "damaged-delivery",
			Description: "Report an item that arrived damaged.",
			FullContent: `## Damage on delivery

Report damage within 7 days of delivery. Supply the order ID and a photo of the damage.
After the report is checked, the customer can request a replacement or a refund.`,
			Keywords:         []string{"damaged", "broken", "replacement"},
			SyntheticQueries: []string{"my parcel arrived with a broken item"},
		},
	}
}

// BuildRunner indexes the policies at startup. Keep the embedder open while the runner is in use.
func BuildRunner(
	ctx context.Context, model gent.Model, embedder search.Embedder, mode policy.SearchResultMode,
) (*executor.Runner, error) {
	policies := SamplePolicies()
	lookup, err := policy.NewPolicySearchTool(ctx, embedder, policies)
	if err != nil {
		return nil, err
	}
	lookup.WithTopK(3).WithResultMode(mode)
	chain := toolchain.New()
	chain.RegisterTool(lookup)
	if mode != policy.SearchResultFull {
		chain.RegisterTool(policy.NewGetPolicyTool(policies))
	}
	term := termination.NewText("answer").WithGuidance(
		"State the policy ID and the conditions that apply. Ask for any missing facts.",
	)
	loop := react.NewAgent(model).
		WithToolChain(chain).
		WithTermination(term).
		WithBehaviorAndContext("Help a customer understand the store's return rules.").
		WithCriticalRules(`Search the policies before you answer a policy question.
If search returns a title or a snippet, use get_policy to read the full policy first.
Use only the policy text you read. Do not approve a refund or invent missing facts.`)
	return executor.NewRunner(loop, executor.DefaultConfig()), nil
}

// Answer uses a new execution context so history and limits belong to this question.
func Answer(ctx context.Context, runner *executor.Runner, question string) (string, error) {
	data := gent.NewBasicLoopData(&gent.Task{Text: question})
	execCtx := gent.NewExecutionContext(ctx, "policy-help", data)
	limits := gent.DefaultLimits()
	for i := range limits {
		if limits[i].Key == gent.SCIterations.Self() {
			limits[i].MaxValue = 6
		}
	}
	execCtx.SetLimits(limits)
	result, err := runner.Execute(execCtx)
	if err != nil {
		return "", err
	}
	if result.TerminationReason != gent.TerminationSuccess {
		return "", fmt.Errorf("run ended with %s: %v", result.TerminationReason, result.Error)
	}
	if len(result.Output) != 1 {
		return "", fmt.Errorf("expected one text answer, got %d parts", len(result.Output))
	}
	text, ok := result.Output[0].(gent.TextContent)
	if !ok {
		return "", fmt.Errorf("expected text answer, got %T", result.Output[0])
	}
	return text.Text, nil
}

func NewEmbedder() (search.Embedder, error) {
	cfg := common.FindConfig("multilingual-e5-small")
	if cfg == nil {
		return nil, fmt.Errorf("embedding configuration not found")
	}
	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		return nil, err
	}
	return search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
		ModelPath:     filepath.Join(dir, cfg.Model.ModelFile),
		TokenizerPath: filepath.Join(dir, "tokenizer.json"), NumThreads: 2, MaxConcurrency: 2,
	})
}

func run() (runErr error) {
	key, modelName := os.Getenv("OPENAI_API_KEY"), os.Getenv("GENT_MODEL")
	if key == "" || modelName == "" {
		return fmt.Errorf("set OPENAI_API_KEY and GENT_MODEL to use this example")
	}
	embedder, err := NewEmbedder()
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, embedder.Close()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	model := models.NewOpenAI(key, modelName)
	runner, err := BuildRunner(ctx, model, embedder, policy.SearchResultFull)
	if err != nil {
		return err
	}
	answer, err := Answer(ctx, runner, "An unused item arrived 10 days ago. Can I return it?")
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
