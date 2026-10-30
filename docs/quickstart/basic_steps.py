"""The basic scenario uses snippets from its complete, tested Go program."""

from pathlib import Path

EXAMPLE = (Path(__file__).resolve().parent / "example/main.go").read_text()


def extract(start, end):
    begin = EXAMPLE.index(start)
    finish = EXAMPLE.index(end, begin)
    return EXAMPLE[begin:finish].rstrip()


STEPS = [
    {
        "id": "setup", "short": "Create the project", "title": "Create a Go project.",
        "intro": "Make a small application with one file, main.go. Use the local Gent checkout "
                 "so the code matches the API used in this guide.",
        "action": "In a terminal, run the project commands below. Then create main.go and add "
                  "these imports. The file will compile after you add the remaining steps.",
        "location": "main.go · package and imports",
        "code": extract("package main", "type OrderInput"),
        "notes": [
            ("gent", "Provides the shared task, model, tool, and execution types."),
            ("agents/react + executor",
             "Provide the standard loop and the runner that executes it."),
            ("schema + toolchain",
             "Describe tool input and register tools."),
        ],
        "done": "You have a Go module that points to your Gent checkout and a main.go file.",
    },
    {
        "id": "model", "short": "Connect a model", "title": "Create the model adapter.",
        "intro": "The standard loop needs a gent.Model. Use a ready-made provider adapter. "
                 "This example uses models.NewOpenAI.",
        "action": "Create a run() error function. Put these lines at its start. Step 7 supplies "
                  "the rest of run() and the main() entry point.",
        "location": "main.go · start of run() · fragment",
        "code": extract('\tkey, modelName :=', '\trunner := newRunner(model)'),
        "notes": [
            ("os.Getenv", "Read credentials and the model name from the process environment."),
            ("models.NewOpenAI(key, modelName)", "Create an adapter that implements gent.Model. "
             "Select a model that supports native tool calls."),
            ("Another provider", "You can pass a different gent.Model to newRunner. "
             "The tool and task code can stay the same."),
        ],
        "done": "You have a model value to pass to the standard ReAct agent.",
    },
    {
        "id": "tool", "short": "Write one tool", "title": "Wrap your application function.",
        "intro": "Give the agent a function it can call to get a fact. Here, lookup_order "
                 "returns a fixed order record. In your app, this function can call your API.",
        "action": "Add these types and the newOrderTool() function after the imports, "
                  "outside run(). Use the JSON field name in each field setting.",
        "location": "main.go · top-level types and newOrderTool()",
        "code": extract("type OrderInput", "// Build the tool registry"),
        "notes": [
            ("OrderInput + order_id",
             "Define the typed input with JSON field names. The builder uses those names "
             "and Go types to create the parameter schema."),
            ("gent.DefineTool", "Add descriptions and constraints with Parameters, then build "
             "gent.Tool[OrderInput, OrderStatus]. MustBuild checks fixed definitions at startup."),
            ("Return a value or an error", "Return OrderStatus on success. The registry formats "
             "structured output as JSON and reports tool errors to the loop."),
        ],
        "done": "You have a typed tool. It is available to the agent after you register it "
                "in step 4.",
    },
    {
        "id": "agent", "short": "Build the agent",
        "title": "Register the tool and create the loop.",
        "intro": "Use Gent’s standard ReAct agent to call the model, execute tools, and check "
                 "the final answer. Your code supplies the tools and instructions.",
        "action": "Add newRunner() after newOrderTool(). Configure it once, then reuse the "
                  "returned runner for separate questions.",
        "location": "main.go · newRunner(model gent.Model)",
        "code": extract("// Build the tool registry", "func ask("),
        "notes": [
            ("toolchain.New() + RegisterTool",
             "Create a gent.ToolChain and make lookup_order available."),
            ("react.NewAgent(model)",
             "Create the standard gent.Loop[string] with TextOutput. You do not implement Next()."),
            ("WithToolChain", "Connect the registry. Task messages supply the instructions. "
             "Use react.NewWithOutput for a different typed output contract."),
            ("executor.NewRunner",
             "Give the loop to the runner. The runner manages repeated iterations."),
        ],
        "done": "The model, tool registry, answer guidance, loop, and runner are connected.",
    },
    {
        "id": "task", "short": "Create a task", "title": "Give each question its own context.",
        "intro": "The task contains the caller-authored system and user messages. ExecutionContext holds this run’s "
                 "history, statistics, and limits.",
        "action": "Start the ask() function below. Step 6 completes its body. Start with "
                  "DefaultLimits so the standard error limits stay in place when you reduce "
                  "the iteration limit.",
        "location": "main.go · start of ask() · completed in step 6",
        "code": extract("func ask(", "\tanswer, result, err := runner.Execute(execCtx)"),
        "notes": [
            ("NewBasicLoopData",
             "Use the standard gent.LoopData implementation to supply the Task."),
            ("NewExecutionContext", "Create a fresh context per question. Do not reuse a completed "
             "execution context for a new question."),
            ("SCIterations.Self()", "Limit this run to six completed iterations. "
             "SetLimits replaces the whole list, so the code first copies the default list."),
        ],
        "done": "You have execCtx with a task and limits, ready to pass to Execute().",
    },
    {
        "id": "execute", "short": "Read the answer",
        "title": "Execute the runner and check the result.",
        "intro": "Execute is the call that starts the agent. It returns when the answer is "
                 "accepted, a limit is reached, the context is canceled, or an error ends the run.",
        "action": "Continue the ask() function with this code. Handle the returned error first. "
                  "Then check the termination reason and read the text output.",
        "location": "main.go · rest of ask() · includes its closing brace",
        "code": extract("\tanswer, result, err := runner.Execute(execCtx)", "func run()"),
        "notes": [
            ("runner.Execute(execCtx)",
             "Run the loop. The runner also releases the execution context when it returns."),
            ("TerminationSuccess",
             "Confirm that the run produced an accepted answer before using it."),
            ("Typed answer", "TextOutput returns a string directly. The execution result also "
             "keeps ordered delivery parts for display and saved replay."),
        ],
        "done": "ask() now returns the final answer or an error to your application.",
    },
    {
        "id": "run", "short": "Run the program", "title": "Connect main() and run your agent.",
        "intro": "Finish run(), add main(), and run the complete application from your terminal. "
                 "Use the full-file view to compare your work or download the complete source.",
        "action": "Use this complete run() function in place of the fragment from step 2. "
                  "Add main() below it. The 60-second timeout cancels the run if it takes "
                  "too long.",
        "location": "main.go · complete run() and main()",
        "code": EXAMPLE[EXAMPLE.index("func run() error"):].rstrip(),
        "notes": [
            ("newRunner(model)", "Build the configuration once. A service can keep this runner and "
             "call ask() with a new context for each request."),
            ("context.WithTimeout", "Set a deadline for the question. Pass the request context "
             "instead of context.Background() when you use this in a server."),
            ("Replace the example tool", "Keep the input and schema aligned. Pass ctx to your API "
             "or database call so it can stop when the run is canceled."),
        ],
        "done": "Your app can ask the model to call your tool and return a text answer.",
    },
]
