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
        "intro": "Make a small application with main.go and fixture.go. Use the local Gent checkout "
                 "so the code matches the API used in this guide.",
        "action": "In a terminal, run the project commands below. Download fixture.go from the complete project, then create main.go and add "
                  "these imports. The file will compile after you add the remaining steps.",
        "location": "main.go · package and imports",
        "code": extract("package main", "type ShipmentInput"),
        "notes": [
            ("gent", "Provides the shared task, model, tool, and execution types."),
            ("agents/react + executor",
             "Provide the standard loop and the runner that executes it."),
            ("toolchain",
             "Describe tool input and register tools."),
        ],
        "done": "You have a Go module that points to your Gent checkout and a main.go file.",
    },
    {
        "id": "model", "short": "Connect a model", "title": "Create the model adapter.",
        "intro": "The standard loop needs a gent.Model. Use a ready-made provider adapter. "
                 "This example uses models.NewOpenAI.",
        "action": "Create a run() error function. Put these lines at its start. The final step supplies "
                  "the rest of run() and the main() entry point.",
        "location": "main.go · start of run() · fragment",
        "code": extract('\tkey, modelName :=', '\trunner := newRunner(model, newDemoShipping())'),
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
        "id": "tool", "short": "Define a tool", "title": "Describe a typed shipment tool.",
        "intro": "The application owns shipment creation and approval. Gent validates the input, "
                 "converts it to ShipmentInput, and saves the effective input before Call.",
        "action": "Add these application types and the constructor. The complete download also "
                  "contains fixture.go with a local service and a valid sample PDF.",
        "location": "main.go · types and NewShipmentTool",
        "code": extract("type ShipmentInput", "func (t *ShipmentTool) Call"),
        "notes": [
            ("Typed input", "Use JSON field names. The builder can infer parameters from a struct "
             "or accept the complete schema shown here."),
            ("ShippingService", "Your service owns approval and the operation ledger. Policy text "
             "is model guidance; it cannot enforce approval."),
            ("DefineTool", "The builder supplies every Tool and IndexableTool metadata method."),
            ("OutputSchemaFrom", "Describe domain data for the model. This does not validate "
             "results, describe the PDF, or expose Gent status fields."),
            ("Safe errors", "A Go error ends this operation. Gent sends this safe text to the model "
             "and saves the raw error privately. The model decides how to continue."),
        ],
        "done": "The tool definition is ready. Next, return an explicit response.",
    },
    {
        "id": "response", "short": "Return parts", "title": "Return domain data and a PDF.",
        "intro": "Use Executed for a domain response and Blocked for a required approval. "
                 "A declined shipment is still an Executed response.",
        "action": "Add Call and result. Gent handles serialization, storage, provider messages, "
                  "and media fallback. The tool author does not convert the struct to JSON.",
        "location": "main.go · Call and result",
        "code": extract("func (t *ShipmentTool) Call", "func (t *ShipmentTool) Recover"),
        "notes": [
            ("Stable identity", "Associate the operation and saved response with this call ID. "
             "A new model request is a new call; recovery inspects the original ID."),
            ("Blocked", "Put the approval reference in Instructions. Gent adds blocked guidance "
             "and excludes response parts for this status."),
            ("NewParts", "Data captures a portable value. PDF retains owned bytes. Build returns "
             "one error and no partial content when capture fails."),
            ("Executed", "The payload states business success. Parts and Instructions reach the "
             "model in order. Unsupported media uses configured safe guidance."),
        ],
        "done": "The normal path handles creation, decline, approval, and unexpected errors.",
    },
    {
        "id": "recovery", "short": "Inspect recovery", "title": "Report evidence after interruption.",
        "intro": "Recover inspects the original operation. It does not execute it again. "
                 "Gent supplies the already typed saved input.",
        "action": "Add Recover. The demo service treats a missing record as Unresolved. It uses "
                  "NotStarted only when its cancellation ledger supplies positive evidence.",
        "location": "main.go · Recover",
        "code": extract("func (t *ShipmentTool) Recover", "const agentPrompt"),
        "notes": [
            ("Typed recovery input", "Use request.Input directly. Tool authors do not decode "
             "saved values or manage checkpoints."),
            ("Completed", "Return the original execution response. An old response does not "
             "prove that shipment information is still current."),
            ("NotStarted", "Return control to the model. Gent does not execute the pending call."),
            ("Unresolved", "Supply optional data or media evidence and instructions. The model "
             "decides whether to inspect again, request approval, or make another plan."),
        ],
        "done": "Execution and recovery now share one complete tool contract.",
    },
    {
        "id": "agent", "short": "Build the agent",
        "title": "Register the tool and create the loop.",
        "intro": "Use Gent’s standard ReAct agent to call the model, execute tools, and check "
                 "the final answer. Your code supplies the tools and instructions.",
        "action": "Add newRunner() after NewShipmentTool(service). Configure it once, then reuse the "
                  "returned runner for separate questions.",
        "location": "main.go · newRunner(model gent.Model)",
        "code": extract("const agentPrompt", "func ask("),
        "notes": [
            ("toolchain.New() + RegisterTool",
             "Create a gent.ToolChain and make create_shipment available."),
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
        "action": "Start the ask() function below. The next step completes its body. Start with "
                  "DefaultLimits so the standard error limits stay in place when you reduce "
                  "the iteration limit.",
        "location": "main.go · start of ask() · completed in the next step",
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
            ("newRunner(model, newDemoShipping())", "Build the configuration once. A service can keep this runner and "
             "call ask() with a new context for each request."),
            ("context.WithTimeout", "Set a deadline for the question. Pass the request context "
             "instead of context.Background() when you use this in a server."),
            ("Replace the example tool", "Keep the input and schema aligned. Pass ctx to your API "
             "or database call so it can stop when the run is canceled."),
        ],
        "done": "Your app can ask the model to call your tool and return a text answer.",
    },
]
