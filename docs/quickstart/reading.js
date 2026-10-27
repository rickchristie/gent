"use strict";

// Prism supplies language grammars. Line wrappers keep source text and clipboard bytes intact.
const wideReading = matchMedia("(min-width: 1100px)");
const svgNamespace = "http://www.w3.org/2000/svg";
let readingFrame = 0;

function escapeCode(value) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function coloredLines(source, language) {
  const lines = [""];
  const grammar = Prism.languages[language];
  const env = { code: source, grammar, language, tokens: [source] };
  if (grammar) {
    Prism.hooks.run("before-tokenize", env);
    env.tokens = Prism.tokenize(env.code, env.grammar);
    Prism.hooks.run("after-tokenize", env);
  }
  function append(value, classes = []) {
    if (Array.isArray(value)) {
      value.forEach((part) => append(part, classes));
      return;
    }
    if (typeof value !== "string") {
      append(value.content, [...classes, value.type, ...[value.alias ?? []].flat()]);
      return;
    }
    value.split("\n").forEach((part, index) => {
      if (index) lines.push("");
      const text = escapeCode(part);
      lines[lines.length - 1] +=
        classes.length && part ? `<span class="token ${classes.join(" ")}">${text}</span>` : text;
    });
  }
  append(env.tokens);
  return lines;
}

function renderCode(element, source, language, start = 1) {
  const lines = coloredLines(source, language);
  const finalBreak = source.endsWith("\n");
  if (finalBreak) lines.pop();
  element.classList.add("syntax-code");
  element.dataset.language = language;
  element.innerHTML =
    lines
      .map((line, index) => {
        const number = start + index;
        return (
          `<span class="code-line" id="${element.id}-L${number}" data-line="${number}" ` +
          `tabindex="-1"><span class="line-text">${line}</span></span>`
        );
      })
      .join("\n") + (finalBreak ? "\n" : "");
}

function lineLabel(note) {
  return note.first === note.last ? `Line ${note.first}` : `Lines ${note.first}–${note.last}`;
}

function renderReading(id, source, reading) {
  const element = document.getElementById(id);
  const study = element.closest(".code-study");
  renderCode(element, source, reading.language, reading.start);
  const list = study.querySelector(".annotation-list");
  const context = study.querySelector(".context-notes");
  list.replaceChildren();
  context.replaceChildren();
  for (const note of reading.notes) {
    if (!note.first) {
      const paragraph = document.createElement("p");
      const label = document.createElement("strong");
      label.textContent = `${note.title}. `;
      paragraph.append(label, note.text);
      context.append(paragraph);
      continue;
    }
    const item = document.createElement("li");
    item.className = "annotation";
    item.dataset.first = note.first;
    item.dataset.last = note.last;
    const button = document.createElement("button");
    button.className = "annotation-jump";
    button.setAttribute("aria-label", `Show ${lineLabel(note).toLowerCase()}: ${note.title}`);
    button.setAttribute("aria-controls", `${id}-L${note.first}`);
    const arrow = document.createElement("span");
    arrow.className = "note-arrow";
    arrow.setAttribute("aria-hidden", "true");
    arrow.textContent = "↖";
    const number = document.createElement("span");
    number.className = "note-line";
    number.textContent = lineLabel(note);
    const title = document.createElement("strong");
    title.textContent = note.title;
    button.append(arrow, number, title);
    const body = document.createElement("p");
    body.textContent = note.text;
    item.append(button, body);
    list.append(item);
    button.onclick = () => {
      selectNote(item);
      const line = document.getElementById(`${id}-L${note.first}`);
      line.focus({ preventScroll: true });
      line.scrollIntoView({ block: "center", behavior: "instant" });
    };
    item.onpointerenter = () => selectNote(item);
    button.onfocus = () => selectNote(item);
  }
  context.hidden = !context.childElementCount;
  study.querySelector(".annotation-column").hidden = !list.childElementCount;
  scheduleReadingLayout();
}

function selectNote(item) {
  const study = item.closest(".code-study");
  study.querySelectorAll(".annotation").forEach((note) => {
    note.classList.toggle("selected", note === item);
  });
  study.querySelectorAll(".code-line").forEach((line) => {
    const number = Number(line.dataset.line);
    line.classList.toggle(
      "line-focus",
      number >= +item.dataset.first && number <= +item.dataset.last,
    );
  });
  study.querySelectorAll(".connector").forEach((path) => {
    path.classList.toggle(
      "selected",
      path.dataset.note === String([...item.parentNode.children].indexOf(item)),
    );
  });
}

function svgElement(name, attributes) {
  const element = document.createElementNS(svgNamespace, name);
  for (const [key, value] of Object.entries(attributes)) element.setAttribute(key, value);
  return element;
}

function layoutReading(study) {
  const list = study.querySelector(".annotation-list");
  const cards = [...list.children];
  const svg = study.querySelector(".connectors");
  svg.replaceChildren();
  if (!wideReading.matches) {
    list.style.height = "";
    cards.forEach((card) => {
      card.style.top = "";
    });
    return;
  }
  const base = study.getBoundingClientRect();
  const listBox = list.getBoundingClientRect();
  const codeBox = study.querySelector(".code-panel").getBoundingClientRect();
  const id = study.dataset.reader;
  let bottom = 0;
  const paths = [];
  for (const [index, card] of cards.entries()) {
    const line = document.getElementById(`${id}-L${card.dataset.first}`);
    const target = line.getBoundingClientRect();
    // Cards follow source order and stay apart, even when two notes share a source line.
    const top = Math.max(bottom, target.top - listBox.top - 6);
    card.style.top = `${top}px`;
    bottom = top + card.getBoundingClientRect().height + 22;
    const button = card.querySelector("button").getBoundingClientRect();
    const x1 = button.left - base.left - 10;
    const y1 = button.top - base.top + 13;
    const x2 = codeBox.right - base.left - 9;
    const y2 = target.top - base.top + Math.min(target.height / 2, 13);
    const middle = (x1 + x2) / 2;
    paths.push(
      svgElement("path", {
        d: `M ${x1} ${y1} C ${middle} ${y1}, ${middle} ${y2}, ${x2} ${y2}`,
        class: `connector${card.classList.contains("selected") ? " selected" : ""}`,
        "data-note": index,
        "data-line": card.dataset.first,
        "marker-end": `url(#arrow-${id})`,
      }),
    );
  }
  list.style.height = `${bottom}px`;
  const marker = svgElement("marker", {
    id: `arrow-${id}`,
    viewBox: "0 0 8 8",
    refX: "7",
    refY: "4",
    markerWidth: "7",
    markerHeight: "7",
    orient: "auto",
    markerUnits: "userSpaceOnUse",
  });
  marker.append(svgElement("path", { d: "M 1 1 L 7 4 L 1 7", class: "arrow-head" }));
  const defs = svgElement("defs", {});
  defs.append(marker);
  svg.append(defs, ...paths);
  svg.setAttribute("viewBox", `0 0 ${base.width} ${study.getBoundingClientRect().height}`);
}

function scheduleReadingLayout() {
  if (readingFrame) return;
  readingFrame = requestAnimationFrame(() => {
    readingFrame = 0;
    document.querySelectorAll(".step:not([hidden]) .code-study").forEach(layoutReading);
  });
}

window.addEventListener("resize", scheduleReadingLayout);
window.addEventListener("beforeprint", () => {
  document.querySelectorAll(".annotation-list").forEach((list) => {
    list.style.height = "";
  });
});
window.addEventListener("afterprint", scheduleReadingLayout);
