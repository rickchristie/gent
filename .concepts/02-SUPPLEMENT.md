# Advanced LLM Reasoning Patterns: Comprehensive Supplement

This supplement expands on the foundational patterns covered in the main guide, adding critical reasoning patterns that were missing. These patterns represent the cutting edge of prompt engineering and agent design.

---

## Chain-of-Verification (CoVe): Self-Correcting Hallucinations

**Origin**: Dhuliawala et al., Meta AI, 2023 (ACL 2024 Findings)

Chain-of-Verification is a four-stage metacognitive framework that enables LLMs to systematically verify their own outputs, reducing hallucinations by **50-70%** on QA and long-form generation benchmarks.

### How It Works

```
┌─────────────────────────────────────────────────────────────┐
│  1. DRAFT: Generate initial baseline response               │
│     ↓                                                       │
│  2. PLAN: Generate verification questions to fact-check     │
│     ↓                                                       │
│  3. EXECUTE: Answer verification questions INDEPENDENTLY    │
│     (isolated from draft to prevent bias propagation)       │
│     ↓                                                       │
│  4. SYNTHESIZE: Generate final verified response            │
└─────────────────────────────────────────────────────────────┘
```

### Critical Implementation Detail

The **factored execution** variant is essential: each verification question must be answered in a context isolated from the initial draft AND from other verification questions. This prevents the hallucination in the draft stage from "poisoning" the verification stage through confirmation bias.

### Example

```
Query: "Name some politicians who were born in New York City."

1. DRAFT (baseline, prone to hallucination):
   "Hillary Clinton, Michael Bloomberg, Donald Trump..."

2. PLAN (generate verification questions):
   - "Where was Hillary Clinton born?"
   - "Where was Michael Bloomberg born?"
   - "Where was Donald Trump born?"

3. EXECUTE (answer each independently):
   - Hillary Clinton → Chicago, IL (NOT NYC)
   - Michael Bloomberg → Boston, MA (NOT NYC)
   - Donald Trump → Queens, NYC ✓

4. SYNTHESIZE (corrected response):
   "Donald Trump was born in Queens, NYC..."
```

### Performance

| Task | Improvement over Baseline |
|------|--------------------------|
| List-based questions | +23% precision |
| Closed-book QA (MultiSpanQA) | +28% F1 score |
| Long-form generation | Outperforms ChatGPT, InstructGPT |

### When to Use

- **Ideal for**: Factual QA, list generation, biography writing, any task where factual accuracy is critical
- **Avoid for**: Creative tasks, opinion-based content, real-time applications (4 LLM calls add latency)

### Golang Implementation Considerations

```go
type CoVeVerifier struct {
    llm           LLMClient
    maxQuestions  int
}

func (v *CoVeVerifier) Verify(ctx context.Context, query string) (string, error) {
    // Stage 1: Generate baseline
    draft := v.llm.Generate(ctx, query)
    
    // Stage 2: Plan verification questions
    questions := v.llm.Generate(ctx, fmt.Sprintf(
        "Given this response to '%s': %s\n"+
        "Generate %d verification questions to fact-check specific claims.",
        query, draft, v.maxQuestions))
    
    // Stage 3: Execute independently (critical: isolated contexts)
    var verifications []Verification
    for _, q := range parseQuestions(questions) {
        // Each question answered in FRESH context
        answer := v.llm.Generate(ctx, q) // No draft in context!
        verifications = append(verifications, Verification{q, answer})
    }
    
    // Stage 4: Synthesize
    return v.llm.Generate(ctx, fmt.Sprintf(
        "Original query: %s\nDraft: %s\nVerifications: %v\n"+
        "Generate corrected response based on verification results.",
        query, draft, verifications))
}
```

---

## Self-Consistency: Ensemble Reasoning Through Majority Voting

**Origin**: Wang et al., Google, 2022

Self-Consistency replaces greedy decoding in Chain-of-Thought prompting by sampling **multiple diverse reasoning paths** and selecting the most consistent answer through majority voting.

### How It Works

```
                    ┌─→ Reasoning Path 1 → Answer: 42
                    │
Query + CoT Prompt ─┼─→ Reasoning Path 2 → Answer: 42
                    │
                    ├─→ Reasoning Path 3 → Answer: 37
                    │
                    └─→ Reasoning Path 4 → Answer: 42
                    
                    Majority Vote → Final Answer: 42
```

### Key Insight

A complex reasoning problem typically admits **multiple different valid reasoning approaches** that lead to the same correct answer. If different reasoning paths converge on the same answer, that answer is more likely correct.

### Performance Improvements

| Benchmark | CoT Only | CoT + Self-Consistency | Gain |
|-----------|----------|------------------------|------|
| GSM8K | 56.5% | 74.4% | +17.9% |
| SVAMP | 68.9% | 86.6% | +17.7% |
| AQuA | 35.8% | 48.3% | +12.5% |
| ARC-c | 85.2% | 91.0% | +5.8% |

### Implementation Parameters

- **Number of samples (k)**: Typically 5-40 paths. Diminishing returns after ~20.
- **Temperature**: Higher (0.7-1.0) to encourage diversity
- **Voting mechanism**: Simple majority, or weighted by path probability

### When to Use

- **Ideal for**: Math problems, symbolic reasoning, any task with a single correct answer
- **Avoid for**: Open-ended generation (no single "correct" answer to vote on), cost-sensitive applications (k× more API calls)

### Limitation

Cannot handle non-numerical/non-discrete outputs. For creative tasks where answers can't be directly compared, use Self-Refine or Evaluator-Optimizer patterns instead.

---

## Self-Refine: Iterative Feedback Without Training

**Origin**: Madaan et al., CMU/Allen AI, 2023 (NeurIPS 2023)

Self-Refine enables LLMs to iteratively improve outputs through a **generate → feedback → refine** loop, using a single LLM as generator, critic, and refiner. No additional training, RL, or separate models required.

### How It Works

```
┌────────────────────────────────────────────────────────────┐
│                                                            │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐             │
│  │ GENERATE │───→│ FEEDBACK │───→│  REFINE  │──┐          │
│  └──────────┘    └──────────┘    └──────────┘  │          │
│       ↑                                         │          │
│       └─────────────────────────────────────────┘          │
│              (iterate until stopping criteria)             │
└────────────────────────────────────────────────────────────┘
```

### Critical: Actionable Feedback

The feedback must be **actionable** with two components:
1. **Localization**: Where is the problem?
2. **Instruction**: How to fix it?

```
Bad feedback:  "The code could be better."
Good feedback: "Line 5 uses a nested loop O(n²). Replace with 
               a hashmap lookup to achieve O(n)."
```

### Performance

Across 7 diverse tasks, Self-Refine improves outputs by **~20% absolute** over single-generation baselines. Works with GPT-3.5, GPT-4, and other capable models.

| Task | Improvement |
|------|-------------|
| Code Optimization | +8.7% speedup |
| Sentiment Reversal | +17.2% accuracy |
| Dialogue Response | +9.2% appropriateness |
| Math Reasoning | +5.3% accuracy |

### Key Findings

1. **Most gains in early iterations**: 60-80% of improvement happens in iterations 1-2
2. **Stopping criteria matter**: Use task-specific quality thresholds, not fixed iteration counts
3. **Works best with capable models**: Weaker models (e.g., Vicuna-13B) may need more prompt engineering

### When to Use

- **Ideal for**: Code optimization, writing refinement, any task with measurable quality criteria
- **Avoid for**: Tasks where the model is confidently wrong (self-critique may reinforce errors)

### Golang Implementation Pattern

```go
type SelfRefiner struct {
    llm              LLMClient
    maxIterations    int
    qualityThreshold float64
}

func (r *SelfRefiner) Refine(ctx context.Context, task string) (string, error) {
    // Initial generation
    output := r.llm.Generate(ctx, task)
    
    for i := 0; i < r.maxIterations; i++ {
        // Generate feedback
        feedback := r.llm.Generate(ctx, fmt.Sprintf(
            "Task: %s\nCurrent output: %s\n"+
            "Provide specific, actionable feedback. "+
            "If no improvements needed, say 'SATISFIED'.",
            task, output))
        
        if strings.Contains(feedback, "SATISFIED") {
            break
        }
        
        // Refine based on feedback
        output = r.llm.Generate(ctx, fmt.Sprintf(
            "Task: %s\nPrevious output: %s\nFeedback: %s\n"+
            "Generate improved version addressing the feedback.",
            task, output, feedback))
    }
    return output, nil
}
```

---

## Reflexion: Learning from Failure Through Verbal Reinforcement

**Origin**: Shinn et al., 2023

Reflexion extends ReAct by adding **episodic memory of failures** with verbal self-reflection. Instead of just retrying, the agent reflects on WHY it failed and stores this insight for future attempts.

### How It Works

```
┌─────────────────────────────────────────────────────────────┐
│ Trial 1:                                                    │
│   Actor (ReAct) ──→ Trajectory ──→ Evaluator ──→ FAIL      │
│                                         │                   │
│                                         ↓                   │
│                              Self-Reflection:               │
│                              "I failed because I searched   │
│                               for the wrong entity..."      │
│                                         │                   │
│                                         ↓                   │
│                              Store in Memory                │
│                                                             │
│ Trial 2:                                                    │
│   [Memory: "Last time I failed because..."]                │
│   Actor (ReAct) ──→ Trajectory ──→ Evaluator ──→ SUCCESS   │
└─────────────────────────────────────────────────────────────┘
```

### Three Components

1. **Actor**: Generates actions (uses CoT + ReAct)
2. **Evaluator**: Scores trajectory (heuristic or LLM)
3. **Self-Reflection**: Generates verbal feedback stored in long-term memory

### Performance

| Benchmark | ReAct | Reflexion | Gain |
|-----------|-------|-----------|------|
| AlfWorld | 77% | 97% | +20% |
| HotPotQA | 29% | 51% | +22% |
| HumanEval (code) | 80% | 91% | +11% |

### Critical Insight

Self-reflection works because **LLMs can often identify errors even when they can't avoid them initially**. The reflection converts implicit failure signals into explicit corrective knowledge.

### Types of Reflection (from research)

| Reflection Type | Description | Effectiveness |
|-----------------|-------------|---------------|
| Retry | Just try again | Low |
| Keywords | Key terms to remember | Medium |
| Advice | General guidance | Medium |
| Instructions | Step-by-step fix | High |
| Explanation | Why it failed + how to fix | Highest |

### When to Use

- **Ideal for**: Multi-attempt tasks (coding, decision-making), where learning from mistakes is valuable
- **Avoid for**: Single-shot tasks, when model is confidently wrong (may reinforce bad patterns)

---

## Skeleton-of-Thought (SoT): Parallel Generation for Speed

**Origin**: Ning et al., Tsinghua/Microsoft, 2024 (ICLR 2024)

Skeleton-of-Thought reduces generation latency by **2x** through parallel decoding. The LLM first generates an answer skeleton (outline), then expands each point in parallel.

### How It Works

```
Sequential (Traditional):
  Question → [Token 1] → [Token 2] → ... → [Token N]
  Total time: N × token_latency

Skeleton-of-Thought:
  Question → Skeleton (3-10 points) → Parallel Expansion
                    │
                    ├─→ Point 1 → [Expanded content]
                    ├─→ Point 2 → [Expanded content]  } Parallel
                    ├─→ Point 3 → [Expanded content]
                    └─→ ...
  Total time: skeleton_time + max(expansion_times)
```

### Performance

| Model | Speed-up | Quality (vs Sequential) |
|-------|----------|------------------------|
| GPT-4 | 1.86x | +2.3% net win rate |
| Claude | 2.11x | +1.8% net win rate |
| LLaMA-2-70B | 2.39x | -0.5% net win rate |

### Prompts

**Skeleton Stage**:
```
Given the question, provide a skeleton of the answer. 
This should be a list of 3-10 points that cover the main aspects.
Format: 1. [Point 1 title]\n2. [Point 2 title]\n...
```

**Point-Expanding Stage**:
```
You're responsible for continuing the writing of one and 
only one point in the overall answer.
Write it very short in 1-2 sentences.
Continue only point {N}.
```

### When to Use

- **Ideal for**: Long-form content, explanations, recommendations—tasks with naturally parallelizable structure
- **Avoid for**: Sequential reasoning (math, logic), coding, tasks requiring step dependencies

### Limitation

Points are expanded **independently**—no information flows between them. This breaks tasks where later points depend on earlier ones (e.g., multi-step math).

---

## Least-to-Most Prompting: Easy-to-Hard Generalization

**Origin**: Zhou et al., Google, 2022

Least-to-Most addresses CoT's weakness: poor performance on problems **harder than the exemplars**. It explicitly decomposes complex problems into ordered subproblems, solving from simplest to most complex.

### How It Works

**Two Stages**:
1. **Decomposition**: Break problem into subproblems ordered by complexity
2. **Sequential Solution**: Solve each subproblem, using previous answers as context

```
Original Problem: "How many times can Amy slide before the 
                  playground closes?"

Stage 1 - Decomposition:
  Subproblem 1: "What time does the playground open?"
  Subproblem 2: "What time does it close?"
  Subproblem 3: "How long does each slide take?"
  Subproblem 4: "How many slides fit in the time window?"

Stage 2 - Sequential Solution:
  [Solve SP1] → Context
  [Solve SP2 with SP1 context] → Context
  [Solve SP3 with SP1,SP2 context] → Context
  [Solve SP4 with all previous context] → Final Answer
```

### Performance on SCAN (Compositional Generalization)

| Method | Length Split | Add Jump Split |
|--------|--------------|----------------|
| Standard Prompting | 16.2% | 0.0% |
| Chain-of-Thought | 7.5% | 0.0% |
| **Least-to-Most** | **99.7%** | **99.6%** |

### Key Insight

The **explicit ordering** from simple to complex exposes compositional structure that implicit CoT reasoning misses. Each solved subproblem provides grounded context for harder ones.

### When to Use

- **Ideal for**: Symbolic manipulation, compositional tasks, problems requiring generalization beyond training examples
- **Avoid for**: Tasks without clear subproblem decomposition, when decomposition overhead isn't justified

### Difference from Plan-and-Execute

Plan-and-Execute creates a plan but executes steps potentially in parallel or out of order. Least-to-Most enforces **strict sequential dependency** from simplest to most complex.

---

## Step-Back Prompting: Reasoning Through Abstraction

**Origin**: Zheng et al., Google DeepMind, 2023

Step-Back Prompting improves reasoning by first asking a **higher-level abstraction question** before tackling the specific problem. This grounds reasoning in first principles.

### How It Works

```
┌─────────────────────────────────────────────────────────────┐
│ Original Question:                                          │
│   "What happens to the pressure of an ideal gas if          │
│    temperature increases by 2x and volume increases by 8x?" │
│                                                             │
│ Step-Back Question:                                         │
│   "What physical principles are needed to solve this?"      │
│   → Answer: "Ideal Gas Law: PV = nRT"                       │
│                                                             │
│ Abstraction-Grounded Reasoning:                             │
│   Using PV = nRT...                                         │
│   P₁V₁/T₁ = P₂V₂/T₂                                         │
│   P₂ = P₁ × (V₁/V₂) × (T₂/T₁) = P₁ × (1/8) × 2 = P₁/4     │
│   → Pressure decreases by factor of 4                       │
└─────────────────────────────────────────────────────────────┘
```

### Performance

| Task | Baseline | +CoT | +Step-Back | Gain vs CoT |
|------|----------|------|------------|-------------|
| MMLU Physics | 56.8% | 59.2% | 66.9% | +7.7% |
| MMLU Chemistry | 48.5% | 53.1% | 59.6% | +6.5% |
| TimeQA | 31.8% | 39.1% | 58.4% | +19.3% |
| MuSiQue | 33.2% | 36.7% | 42.8% | +6.1% |

### When to Use

- **Ideal for**: STEM problems, knowledge-intensive QA, multi-hop reasoning
- **Avoid for**: Common knowledge questions, already-abstract questions

### Implementation Pattern

```python
# Step 1: Generate step-back question
step_back_q = llm.generate(f"""
Given this question: {original_question}
What are the underlying principles or concepts needed to solve this?
Generate a more general question about these principles.
""")

# Step 2: Answer the step-back question
principles = llm.generate(step_back_q)

# Step 3: Solve original with principles as context
final = llm.generate(f"""
Principles: {principles}
Original question: {original_question}
Using these principles, solve the original question.
""")
```

---

## Graph-of-Thoughts (GoT): Beyond Trees to Arbitrary Reasoning Graphs

**Origin**: Besta et al., ETH Zürich, 2024 (AAAI 2024)

Graph-of-Thoughts generalizes CoT (chain) and ToT (tree) by modeling LLM reasoning as an **arbitrary graph** where thoughts can be combined, refined through loops, and distilled.

### Comparison of Paradigms

```
Chain-of-Thought:    A → B → C → D
                     (linear sequence)

Tree-of-Thoughts:    A → B → D
                         ↘ → E
                     A → C → F
                     (branching, backtracking)

Graph-of-Thoughts:   A → B ─→ D ←─ C
                         ↘   ↗     ↑
                          → E ─────┘
                     (arbitrary connections, merging, loops)
```

### Key Operations

| Operation | Description |
|-----------|-------------|
| **Generate** | Create new thoughts from existing ones |
| **Aggregate** | Merge multiple thoughts into one |
| **Refine** | Improve a thought through feedback loop |
| **Score** | Evaluate thought quality |
| **KeepBest(N)** | Preserve top N thoughts |

### Performance (Sorting Task)

| Method | Quality | Cost | Speed |
|--------|---------|------|-------|
| CoT | 0.35 | Low | Fast |
| ToT | 0.60 | High | Slow |
| **GoT** | **0.97** | Medium | Medium |

GoT achieves **62% better quality** than ToT while reducing costs by **31%**.

### When to Use

- **Ideal for**: Complex problems benefiting from thought combination, tasks needing iterative refinement
- **Avoid for**: Simple tasks where overhead isn't justified

### Golang Implementation Sketch

```go
type ThoughtGraph struct {
    nodes map[string]*Thought
    edges map[string][]string  // adjacency list
}

type GoTEngine struct {
    graph     *ThoughtGraph
    llm       LLMClient
    scorer    func(*Thought) float64
}

func (e *GoTEngine) Aggregate(thoughtIDs []string) *Thought {
    thoughts := e.getThoughts(thoughtIDs)
    combined := e.llm.Generate(ctx, fmt.Sprintf(
        "Combine these ideas into a single improved solution:\n%v",
        thoughts))
    return &Thought{Content: combined}
}

func (e *GoTEngine) RefineLoop(t *Thought, maxIter int) *Thought {
    for i := 0; i < maxIter; i++ {
        feedback := e.llm.Generate(ctx, "Critique: " + t.Content)
        refined := e.llm.Generate(ctx, 
            "Improve based on feedback:\n" + feedback)
        if e.scorer(refined) <= e.scorer(t) {
            break  // No improvement
        }
        t = refined
    }
    return t
}
```

---

## Program-Aided Language (PAL) / Program of Thoughts (PoT)

**Origin**: Gao et al. (PAL) 2023, Chen et al. (PoT) 2022

PAL and PoT offload computation to external interpreters. The LLM generates **code** as intermediate reasoning steps, then a Python interpreter computes the final answer.

### Key Insight

LLMs are good at **decomposing problems into steps** but struggle with **arithmetic and iteration**. Code interpreters handle computation perfectly.

### How It Works

```
┌─────────────────────────────────────────────────────────────┐
│ Question: "Roger has 5 balls. He buys 2 cans with 3 balls  │
│           each. How many balls total?"                      │
│                                                             │
│ CoT Approach (LLM does math):                               │
│   "5 + 2 × 3 = 5 + 6 = 11" (may make errors on complex math)│
│                                                             │
│ PAL Approach (LLM writes code):                             │
│   ```python                                                 │
│   initial_balls = 5                                         │
│   cans = 2                                                  │
│   balls_per_can = 3                                         │
│   total = initial_balls + (cans * balls_per_can)            │
│   print(total)  # Interpreter: 11                           │
│   ```                                                       │
└─────────────────────────────────────────────────────────────┘
```

### Performance

| Benchmark | CoT | PAL | Gain |
|-----------|-----|-----|------|
| GSM8K | 57% | 72% | +15% |
| SVAMP | 69% | 79% | +10% |
| AQUA | 36% | 47% | +11% |

PAL achieves **>90% accuracy** on 4 of 5 arithmetic benchmarks, nearly solving them.

### When to Use

- **Ideal for**: Math problems, data manipulation, algorithmic tasks, anything requiring precise computation
- **Avoid for**: Tasks requiring world knowledge, common sense reasoning, or qualitative judgments

### Difference: PAL vs PoT

| Aspect | PAL | PoT |
|--------|-----|-----|
| Output | Pure code | Interleaved code + NL comments |
| Execution | Single program | May have multiple execution blocks |
| Use case | Direct computation | Reasoning with computation |

---

## Analogical Prompting: Self-Generated Exemplars

**Origin**: Yasunaga et al., Google DeepMind, 2023

Analogical Prompting has the LLM **self-generate relevant example problems and solutions** before solving the target problem. This eliminates the need for manually curated few-shot examples.

### How It Works

```
┌─────────────────────────────────────────────────────────────┐
│ Prompt:                                                     │
│ "# Problem: Find the area of a square with vertices at      │
│  (-2,2), (2,-2), (-2,-6), (-6,-2)                          │
│                                                             │
│  # Instructions:                                            │
│  ## Relevant problems: Recall 3 relevant and distinct       │
│     problems. For each, describe it and explain solution.   │
│  ## Solve the initial problem."                             │
│                                                             │
│ LLM Self-Generates:                                         │
│ "Relevant Problem 1: Find area of square with side 5.       │
│  Solution: Area = 5² = 25                                   │
│                                                             │
│  Relevant Problem 2: Find distance between (0,0) and (3,4). │
│  Solution: d = √(3² + 4²) = 5                              │
│  ..."                                                       │
│                                                             │
│ Then Solves Original:                                       │
│ "Using distance formula, side = √32. Area = 32."           │
└─────────────────────────────────────────────────────────────┘
```

### Performance

| Task | 0-shot CoT | Few-shot CoT | Analogical |
|------|------------|--------------|------------|
| GSM8K | 78.1% | 80.4% | **83.7%** |
| MATH | 41.2% | 43.8% | **46.9%** |
| Codeforces | 15.2% | 18.6% | **21.4%** |

### Key Advantages

1. **No labeled examples needed**: LLM generates its own
2. **Tailored to each problem**: Examples are specifically relevant
3. **Knowledge recall**: Forces model to activate relevant knowledge

### Optimal Parameters

- **3-5 examples**: Fewer is insufficient, more causes dilution
- **Diverse examples**: Explicitly request "distinct" problems
- **Knowledge before exemplars**: Generate high-level principles first, then specific examples

### Failure Modes (from error analysis)

| Failure Type | Frequency |
|--------------|-----------|
| Exemplars relevant but LLM still fails (generalization gap) | 24% |
| Overfitting to specific exemplars | 16% |
| Other issues | 16% |
| Exemplars correct and problem solved | 44% |

---

## Pattern Selection Guide

### Decision Matrix

| If you need... | Use... | Because... |
|----------------|--------|------------|
| Reduce hallucinations | **CoVe** | Systematic fact-checking |
| Higher accuracy on math | **Self-Consistency** | Ensemble voting |
| Iterative improvement | **Self-Refine** | Generate-feedback-refine loop |
| Learn from failures | **Reflexion** | Episodic memory of mistakes |
| Faster generation | **Skeleton-of-Thought** | Parallel expansion |
| Handle harder problems | **Least-to-Most** | Ordered decomposition |
| Ground in principles | **Step-Back** | Abstraction before details |
| Complex reasoning graphs | **Graph-of-Thoughts** | Thought combination/loops |
| Precise computation | **PAL/PoT** | Code interpreter execution |
| No labeled examples | **Analogical** | Self-generated exemplars |

### Composition Strategies

These patterns can be **combined**:

1. **CoVe + Self-Consistency**: Vote on verification questions
2. **Reflexion + Self-Refine**: Learn from failures AND iterate within trials
3. **PAL + Self-Consistency**: Generate multiple programs, vote on outputs
4. **Step-Back + Least-to-Most**: Abstract principles, then decompose
5. **Analogical + CoT**: Self-generate examples, then reason step-by-step

### Cost-Performance Tradeoffs

| Pattern | LLM Calls | Latency | Quality Gain |
|---------|-----------|---------|--------------|
| CoT | 1 | Low | +10-20% |
| Self-Consistency (k=10) | 10 | Medium | +15-20% |
| CoVe | 4+ | High | +20-30% |
| Self-Refine (3 iter) | 7+ | High | +20% |
| SoT | 1 + N parallel | Low | +speed, ≈quality |
| ToT | 10-100+ | Very High | +30-50% |

---

## Implementation Recommendations for Your Golang Framework

### Core Abstractions

```go
// ReasoningPattern interface for all patterns
type ReasoningPattern interface {
    Execute(ctx context.Context, input string) (Output, error)
    EstimateCost(input string) CostEstimate
}

// Composable patterns
type ComposedPattern struct {
    patterns []ReasoningPattern
    combiner func([]Output) Output
}

// Pattern-specific configs
type CoVeConfig struct {
    MaxVerificationQuestions int
    FactoredExecution       bool  // Critical: isolated contexts
}

type SelfConsistencyConfig struct {
    NumPaths    int
    Temperature float64
    VotingMethod string  // "majority", "weighted"
}

type SelfRefineConfig struct {
    MaxIterations    int
    QualityThreshold float64
    FeedbackPrompt   string
}
```

### Selection Logic

```go
func SelectPattern(task TaskType, constraints Constraints) ReasoningPattern {
    switch {
    case task.RequiresFactualAccuracy && !constraints.Latency:
        return NewCoVe(config)
    case task.HasSingleCorrectAnswer && constraints.Cost.Allows(10):
        return NewSelfConsistency(10)
    case task.RequiresComputation:
        return NewPAL(pythonInterpreter)
    case task.IsMultiStep && task.DifficultyVaries:
        return NewLeastToMost()
    default:
        return NewChainOfThought()  // Safe default
    }
}
```

---

## Summary

These advanced reasoning patterns represent the current frontier of prompt engineering. Key takeaways:

1. **No single pattern is universally best** — match pattern to task characteristics
2. **Composition often beats individual patterns** — combine strategically
3. **Cost scales with capability** — more sophisticated reasoning = more LLM calls
4. **Simpler patterns first** — only add complexity when simpler approaches fail
5. **Verification patterns (CoVe, Self-Consistency) are underutilized** — especially valuable for production systems

For your Golang agent framework, implement these as composable modules with clear interfaces. The pattern selection logic should be configurable and potentially learned from usage data over time.