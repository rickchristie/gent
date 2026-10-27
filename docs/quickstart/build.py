"""Build one offline guide from the exact source files used by the example tests."""

import base64
import hashlib
import html
import io
import json
import re
import subprocess
import textwrap
import zipfile
from pathlib import Path

from basic_steps import STEPS
from scenarios import EXTRA_SCENARIOS
from annotations import file_reading, step_reading

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
OUTPUT = HERE / "dist/gent-quick-start.html"

SCENARIOS = [{
    "id": "basic", "name": "Simple agent", "tag": "01 / START HERE",
    "summary": "One Go file. A tool, an agent, and an answer.",
    "goal": "a program that looks up order-1001 and answers with its status.",
    "project": "example", "steps": STEPS,
}, *EXTRA_SCENARIOS]


def safe(value):
    return html.escape(value, quote=True)


def wrapped(value):
    return textwrap.fill(safe(value), width=100, break_long_words=False, break_on_hyphens=False)


def script_data(value):
    # Split string literals so the standalone artifact stays readable without a network loader.
    def wrap(match):
        chunks, chunk = [], ""
        for character in json.loads(match[0]):
            encoded = json.dumps(character, ensure_ascii=False)[1:-1].replace("<", "\\u003c")
            if len(chunk) + len(encoded) > 60:
                chunks.append('"' + chunk + '"')
                chunk = ""
            chunk += encoded
        chunks.append('"' + chunk + '"')
        return " +\n        ".join(chunks)
    return re.sub(r'"(?:\\.|[^"\\])*"', wrap, json.dumps(value, ensure_ascii=False, indent=2))


def project_data(name):
    files = {}
    for path in sorted((HERE / name).rglob("*")):
        relative = path.relative_to(HERE / name)
        if any(part in {"node_modules", "dist", "__pycache__"} for part in relative.parts):
            continue
        if path.is_file() and path.suffix in {
            ".go", ".mod", ".sum", ".json", ".ts", ".tsx", ".html", ".css", ".md"}:
            files[str(relative)] = path.read_text()
    if name == "example":
        download = files["main.go"].encode()
        filename, mime = "main.go", "text/plain;charset=utf-8"
        choices = ["main.go"]
    else:
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as bundle:
            for path, content in files.items():
                info = zipfile.ZipInfo(f"{name}/{path}", (2026, 10, 26, 0, 0, 0))
                info.compress_type = zipfile.ZIP_DEFLATED
                bundle.writestr(info, content)
        download = archive.getvalue()
        filename, mime = f"gent-{name}-quick-start.zip", "application/zip"
        choices = [path for path in files if not path.endswith(
            ("_test.go", ".test.ts", "go.sum", "package-lock.json", "trace-fixture.json"))]
        first = "main.go" if name == "server" else "src/main.tsx"
        choices = [first, *[path for path in choices if path != first]]
    return dict(files=files, choices=choices, downloadName=filename, mime=mime,
                download=base64.b64encode(download).decode())


def basic_extra(step):
    if step["id"] == "setup":
        return """<div class="setup-box">
<label for="checkout-path">Path to your Gent checkout</label>
<input id="checkout-path" type="text" value="/absolute/path/to/gent" spellcheck="false">
<p class="small">Use your own folder path. These commands are for Bash or a compatible shell.</p>
<div class="code-head"><span>Terminal · new application folder</span>
<button data-copy="setup-commands">Copy commands</button></div>
<pre id="setup-commands"></pre>
<p class="small">Use Go 1.24.10 or later. The local replacement uses this checkout's API.</p>
</div>"""
    if step["id"] == "run":
        return """<div class="setup-box">
<h2>Run after you save main.go</h2>
<p>Set both values. This command makes live model API requests.</p>
<div class="code-head"><span>Terminal · inside gent-demo</span>
<button data-copy="run-commands">Copy commands</button></div>
<pre id="run-commands">go mod tidy
export OPENAI_API_KEY='your-api-key'
export GENT_MODEL='your-model-name'
CGO_ENABLED=0 go run .</pre>
<p class="small">This build needs no native libraries. Example answer:</p>
<blockquote>Order order-1001 has shipped.</blockquote>
<p>The words can vary. The order record is fixed demo data.</p>
</div>"""
    return ""


def render_study(code_id, code, location, reading, complete=False):
    controls = f'<button data-copy="{code_id}">Copy code</button>'
    if complete:
        key = code_id.removeprefix("complete-")
        controls = f'''<div><button data-copy="{code_id}">Copy full file</button>
<button data-download-file="{key}">Download file</button></div>'''
    name_id = f' id="file-name-{key}"' if complete else ""
    fallback = "\n".join(
        f'<li class="annotation">\n<strong>{safe(item["title"])}</strong>\n'
        f'<p>\n{wrapped(item["text"])}\n</p></li>' for item in reading["notes"])
    return f'''<div class="code-study" data-reader="{code_id}">
<div class="code-panel">
<div class="code-head"><span{name_id}>{safe(location)}</span>{controls}</div>
<div class="code-caption"><span>{safe(reading['language'].upper())}</span>
<span>Source line numbers · Select a note to focus</span></div>
<pre><code id="{code_id}"
>{html.escape(code, quote=False)}</code></pre></div>
<section class="annotation-column" aria-label="Line explanations">
<div class="annotation-head"><span>READ THE CODE</span><h2>Line by line</h2></div>
<ol class="annotation-list">{fallback}</ol></section>
<svg class="connectors" aria-hidden="true"></svg>
<div class="context-notes"></div>
</div>'''


def render_step(scenario, step, index):
    key = f"{scenario['id']}-{step['id']}"
    code_id = f"code-{key}"
    study = render_study(code_id, step["code"], step["location"], readers[code_id])
    extra = basic_extra(step) if scenario["id"] == "basic" else step.get("extra", "")
    extra = textwrap.fill(extra, width=100, break_long_words=False,
                          break_on_hyphens=False) if scenario["id"] != "basic" else extra
    return f"""<section class="step" id="{key}" aria-labelledby="title-{key}">
<div class="eyebrow">{safe(scenario['tag'])} <span>STEP {index + 1:02d}</span></div>
<h1 id="title-{key}">{safe(step['title'])}</h1>
<p class="lead">
{wrapped(step['intro'])}
</p>
<div class="instruction"><span>WHAT TO CODE</span>
<p>
{wrapped(step['action'])}
</p></div>
{extra if step['id'] in {'setup', 'cursor'} else ''}
{study}
{extra if step['id'] not in {'setup', 'cursor'} else ''}
<div class="checkpoint"><span>RESULT</span>
<p>
{wrapped(step['done'])}
</p></div>
</section>"""


def render_files(scenario, project):
    key = scenario["id"]
    path = project["choices"][0]
    study = render_study(f"complete-{key}", project["files"][path], path,
                         project["reading"][path], complete=True)
    options = "\n".join(f'<option value="{safe(path)}">{safe(path)}</option>'
                        for path in project["choices"])
    return f"""<section class="step" id="{key}-files" aria-labelledby="title-{key}-files">
<div class="eyebrow">{safe(scenario['tag'])} <span>COMPLETE SOURCE</span></div>
<h1 id="title-{key}-files">All the pieces, ready to use.</h1>
<p class="lead">These files are the source used by the example tests. Download the source
or copy a file into the matching path. The server and client archives include a README with
run commands.</p>
<label class="file-label" for="select-{key}">Choose a source file</label>
<select id="select-{key}" data-files="{key}">
{options}
</select>
{study}
<button class="primary" data-download-project="{scenario['project']}">Download project</button>
<p class="small">The HTML guide works offline. Building the examples requires Go or Node.js
and their dependencies. Scenario 2 includes a local model that needs no API credentials.</p>
</section>"""


projects = {name: project_data(name) for name in dict.fromkeys(
    item["project"] for item in SCENARIOS)}
readers = {f"code-{scenario['id']}-{step['id']}": step_reading(scenario, step)
           for scenario in SCENARIOS for step in scenario["steps"]}
for name, project in projects.items():
    project["reading"] = {path: file_reading(f"{name}/{path}", project["files"][path], readers)
                          for path in project["choices"]}
source_files = ["agent.go", "model.go", "run.go", "checkpoint.go", "context.go",
                "trace/types.go", "trace/projection.go", "trace/replay.go",
                "trace/subscription.go", "executor/checkpoint.go"]
digest = hashlib.sha256()
for path in source_files:
    digest.update(path.encode())
    digest.update((ROOT / path).read_bytes())
data = {
    "scenarios": [{key: value for key, value in item.items() if key != "steps"} | {
        "steps": [{"id": step["id"], "short": step["short"]} for step in item["steps"]]
    } for item in SCENARIOS],
    "projects": projects, "readers": readers, "checkout": "/absolute/path/to/gent",
    "revision": subprocess.check_output(["git", "rev-parse", "--short", "HEAD"],
                                        cwd=ROOT, text=True).strip(),
    "digest": digest.hexdigest()[:16],
}
sections = []
for scenario in SCENARIOS:
    sections.extend(render_step(scenario, step, index)
                    for index, step in enumerate(scenario["steps"]))
    sections.append(render_files(scenario, projects[scenario["project"]]))
template = (HERE / "template.html").read_text()
for name, value in {
    "CSS": (HERE / "style.css").read_text(), "JS": (HERE / "app.js").read_text(),
    "DATA": script_data(data), "STEPS": "\n".join(sections),
    "PRISM": (HERE / "vendor/prism.js").read_text(),
    "READING": (HERE / "reading.js").read_text(),
}.items():
    template = template.replace(f"@@{name}@@", value)
if re.search(r"@@[A-Z]+@@", template):
    raise ValueError("A template slot was not filled")
OUTPUT.parent.mkdir(exist_ok=True)
OUTPUT.write_text(template)
print(f"Built {OUTPUT.relative_to(ROOT)} ({len(template.encode()):,} bytes)")
