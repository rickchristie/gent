"use strict";

const byId = (id) => document.getElementById(id);
const all = (selector) => [...document.querySelectorAll(selector)];
let scenario = guideData.scenarios[0];
let active = "setup";

function selectedRoute() {
  const hash = location.hash.slice(1);
  const [name, page] = hash.split("/");
  const found = guideData.scenarios.find((item) => item.id === name);
  if (found) return [found, page || found.steps[0].id];
  // Preserve links from the first version of this guide.
  return [guideData.scenarios[0], hash === "complete" ? "files" : hash || "setup"];
}

function showStep(moveFocus = false) {
  const [selected, page] = selectedRoute();
  scenario = selected;
  const ids = [...scenario.steps.map((step) => step.id), "files"];
  active = ids.includes(page) ? page : ids[0];
  all(".step").forEach((section) => {
    section.hidden = section.id !== `${scenario.id}-${active}`;
  });
  byId("steps").replaceChildren(
    ...scenario.steps.map((step, index) => {
      const link = document.createElement("a");
      link.href = `#${scenario.id}/${step.id}`;
      const number = document.createElement("span");
      number.textContent = String(index + 1).padStart(2, "0");
      link.append(number, step.short);
      if (active === step.id) link.setAttribute("aria-current", "step");
      return link;
    }),
  );
  all("#scenarios a").forEach((link) => {
    if (link.dataset.scenario === scenario.id) link.setAttribute("aria-current", "page");
    else link.removeAttribute("aria-current");
  });
  byId("side-label").textContent = scenario.tag;
  byId("side-summary").textContent = scenario.summary;
  byId("goal-text").textContent = scenario.goal;
  byId("file-link").href = byId("files-footer").href = `#${scenario.id}/files`;
  byId("previous").disabled = active === ids[0];
  byId("next").hidden = active === "files";
  byId("next").textContent = active === ids.at(-2) ? "View complete files →" : "Next step →";
  byId("download-top").textContent =
    scenario.id === "basic" ? "Download main.go" : "Download project";
  byId("copy-status").textContent = "";
  scheduleReadingLayout();
  if (moveFocus) {
    byId("main").focus({ preventScroll: true });
    window.scrollTo({ top: 0, behavior: "instant" });
  }
}

function goTo(route) {
  location.hash = route;
  showStep(true);
}

byId("scenarios").replaceChildren(
  ...guideData.scenarios.map((item) => {
    const link = document.createElement("a");
    link.href = `#${item.id}/${item.steps[0].id}`;
    link.dataset.scenario = item.id;
    link.textContent = item.name;
    return link;
  }),
);
document.addEventListener("click", (event) => {
  const link = event.target.closest('a[href^="#"]');
  if (!link) return;
  event.preventDefault();
  if (link.classList.contains("skip")) byId("main").focus();
  else goTo(link.hash.slice(1));
});
byId("next").onclick = () => {
  const ids = [...scenario.steps.map((step) => step.id), "files"];
  goTo(`${scenario.id}/${ids[ids.indexOf(active) + 1]}`);
};
byId("previous").onclick = () => {
  const ids = [...scenario.steps.map((step) => step.id), "files"];
  goTo(`${scenario.id}/${ids[ids.indexOf(active) - 1]}`);
};
window.addEventListener("hashchange", () => {
  const [item, step] = selectedRoute();
  if (item.id !== scenario.id || step !== active) showStep(true);
});

function shellQuote(value) {
  return `'${value.replaceAll("'", "'\\''")}'`;
}
function renderSetup() {
  const path = byId("checkout-path").value.trim();
  const source = `mkdir gent-demo
cd gent-demo
go mod init example.com/gent-demo
go mod edit ${shellQuote(`-replace=github.com/rickchristie/gent=${path}`)}
go get github.com/rickchristie/gent`;
  renderCode(byId("setup-commands"), source, "bash");
}
byId("checkout-path").value = guideData.checkout;
byId("checkout-path").oninput = renderSetup;

async function copyCode(id) {
  const element = byId(id);
  try {
    if (!navigator.clipboard?.writeText) throw new Error("Clipboard is unavailable");
    await navigator.clipboard.writeText(element.textContent);
    byId("copy-status").textContent = "Copied. Paste this into your editor or terminal.";
  } catch (error) {
    const range = document.createRange();
    range.selectNodeContents(element);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    byId("copy-status").textContent = `${error.message}. Text selected; use your Copy command.`;
  }
}
all("[data-copy]").forEach((button) => {
  button.onclick = () => copyCode(button.dataset.copy);
});

function download(name, content, mime) {
  const url = URL.createObjectURL(new Blob([content], { type: mime }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function downloadProject(name) {
  const project = guideData.projects[name];
  const bytes = Uint8Array.from(atob(project.download), (character) => character.charCodeAt(0));
  download(project.downloadName, bytes, project.mime);
}
for (const item of guideData.scenarios) {
  const project = guideData.projects[item.project];
  const select = byId(`select-${item.id}`);
  const renderFile = () => {
    byId(`file-name-${item.id}`).textContent = select.value;
    const study = byId(`complete-${item.id}`).closest(".code-study");
    study.querySelector(".code-caption span").textContent =
      project.reading[select.value].language.toUpperCase();
    renderReading(
      `complete-${item.id}`,
      project.files[select.value],
      project.reading[select.value],
    );
  };
  select.onchange = renderFile;
  renderFile();
}
all("[data-download-file]").forEach((button) => {
  button.onclick = () => {
    const item = guideData.scenarios.find((value) => value.id === button.dataset.downloadFile);
    const path = byId(`select-${item.id}`).value;
    download(path.split("/").at(-1), guideData.projects[item.project].files[path], "text/plain");
  };
});
all("[data-download-project]").forEach((button) => {
  button.onclick = () => downloadProject(button.dataset.downloadProject);
});
byId("download-top").onclick = () => downloadProject(scenario.project);
byId("print").onclick = () => window.print();
byId("source-note").textContent =
  `Built from Gent commit ${guideData.revision}. ` +
  `API source digest: ${guideData.digest}. Use this checkout's API when running the examples.`;
renderSetup();
renderCode(byId("run-commands"), byId("run-commands").textContent, "bash");
for (const [id, reading] of Object.entries(guideData.readers)) {
  renderReading(id, byId(id).textContent, reading);
}
document.documentElement.classList.add("js");
showStep();
