const state = {
  currentJobId: null,
  pollTimer: null,
  jobs: []
};

const form = document.querySelector("#jobForm");
const videoInput = document.querySelector("#videoInput");
const fileTitle = document.querySelector("#fileTitle");
const fileMeta = document.querySelector("#fileMeta");
const dropZone = document.querySelector("#dropZone");
const statusStrip = document.querySelector("#statusStrip");
const toolList = document.querySelector("#toolList");
const startButton = document.querySelector("#startButton");
const cancelButton = document.querySelector("#cancelButton");
const refreshHealth = document.querySelector("#refreshHealth");
const currentJobMeta = document.querySelector("#currentJobMeta");
const stageText = document.querySelector("#stageText");
const progressText = document.querySelector("#progressText");
const progressFill = document.querySelector("#progressFill");
const outputPath = document.querySelector("#outputPath");
const downloadLink = document.querySelector("#downloadLink");
const jobList = document.querySelector("#jobList");
const jobCount = document.querySelector("#jobCount");
const logOutput = document.querySelector("#logOutput");
const logMeta = document.querySelector("#logMeta");
const toast = document.querySelector("#toast");
const resourceTitle = document.querySelector("#resourceTitle");
const resourceLevel = document.querySelector("#resourceLevel");
const resourceSummary = document.querySelector("#resourceSummary");
const resourceCPU = document.querySelector("#resourceCPU");
const resourceGPU = document.querySelector("#resourceGPU");
const resourceDisk = document.querySelector("#resourceDisk");

refreshHealth.addEventListener("click", () => loadHealth());

videoInput.addEventListener("change", () => updateSelectedFile());

for (const option of form.querySelectorAll('input[name="mode"], input[name="preset"]')) {
  option.addEventListener("change", updateResourceHint);
}

dropZone.addEventListener("dragover", (event) => {
  event.preventDefault();
  dropZone.classList.add("is-dragging");
});

dropZone.addEventListener("dragleave", () => {
  dropZone.classList.remove("is-dragging");
});

dropZone.addEventListener("drop", (event) => {
  event.preventDefault();
  dropZone.classList.remove("is-dragging");
  const file = event.dataTransfer.files?.[0];
  if (!file) return;
  const transfer = new DataTransfer();
  transfer.items.add(file);
  videoInput.files = transfer.files;
  updateSelectedFile();
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!videoInput.files?.length) {
    showToast("Choose a video file.");
    videoInput.focus();
    return;
  }

  startButton.disabled = true;
  const data = new FormData(form);
  try {
    const response = await fetch("/api/jobs", {
      method: "POST",
      body: data
    });
    const payload = await parseJSON(response);
    if (!response.ok) throw new Error(payload.error || "Could not start job.");
    state.currentJobId = payload.id;
    renderJob(payload);
    startPolling();
    showToast("Job started.");
  } catch (error) {
    showToast(error.message);
  } finally {
    startButton.disabled = false;
  }
});

cancelButton.addEventListener("click", async () => {
  if (!state.currentJobId) return;
  cancelButton.disabled = true;
  try {
    const response = await fetch(`/api/jobs/${state.currentJobId}/cancel`, {
      method: "POST"
    });
    const payload = await parseJSON(response);
    if (!response.ok) throw new Error(payload.error || "Could not cancel job.");
    renderJob(payload);
    showToast("Cancel requested.");
  } catch (error) {
    showToast(error.message);
  }
});

loadHealth();
loadJobs();
startPolling();
updateResourceHint();

function updateSelectedFile() {
  const file = videoInput.files?.[0];
  if (!file) {
    fileTitle.textContent = "DROP VIDEO STREAM";
    fileMeta.textContent = "click to browse · MP4 / MOV / MKV / WEBM";
    dropZone.classList.remove("has-file");
    return;
  }
  fileTitle.textContent = file.name;
  fileMeta.textContent = `${formatBytes(file.size)} · ${file.type || "video"}`;
  dropZone.classList.add("has-file");
}

async function loadHealth() {
  try {
    const response = await fetch("/api/health");
    const payload = await parseJSON(response);
    if (!response.ok) throw new Error(payload.error || "Tool status failed.");
    renderHealth(payload);
  } catch (error) {
    statusStrip.innerHTML = "";
    toolList.innerHTML = "";
    showToast(error.message);
  }
}

async function loadJobs() {
  try {
    const response = await fetch("/api/jobs");
    const payload = await parseJSON(response);
    if (!response.ok) throw new Error(payload.error || "Could not load jobs.");
    state.jobs = payload;
    renderJobs();
    if (!state.currentJobId && payload.length) {
      state.currentJobId = payload[payload.length - 1].id;
      renderJob(payload[payload.length - 1]);
    }
  } catch {
    state.jobs = [];
    renderJobs();
  }
}

function startPolling() {
  if (state.pollTimer) return;
  state.pollTimer = window.setInterval(async () => {
    await loadJobs();
    if (!state.currentJobId) return;
    try {
      const response = await fetch(`/api/jobs/${state.currentJobId}`);
      const payload = await parseJSON(response);
      if (response.ok) renderJob(payload);
    } catch {
      // Keep the last rendered state.
    }
  }, 1000);
}

function renderHealth(payload) {
  const fastClass = payload.readyForFast ? "ok" : "warn";
  const aiClass = payload.readyForAI ? "ok" : "warn";
  statusStrip.innerHTML = `
    <span class="status-pill ${fastClass}"><span class="status-dot"></span>Fast ${payload.readyForFast ? "ready" : "missing"}</span>
    <span class="status-pill ${aiClass}"><span class="status-dot"></span>AI ${payload.readyForAI ? "ready" : "missing"}</span>
  `;

  toolList.innerHTML = payload.tools.map((tool) => {
    const cls = tool.found ? "ok" : "missing";
    const detail = tool.found ? escapeHTML(tool.path || tool.version || "Available") : escapeHTML(tool.install || tool.message);
    return `
      <div class="tool-row ${cls}">
        <span class="status-dot"></span>
        <div>
          <strong>${escapeHTML(tool.name)}</strong>
          <span>${detail}</span>
        </div>
      </div>
    `;
  }).join("");

  for (const row of toolList.querySelectorAll(".tool-row.ok .status-dot")) {
    row.style.background = "var(--accent)";
  }
  for (const row of toolList.querySelectorAll(".tool-row.missing .status-dot")) {
    row.style.background = "var(--danger)";
  }
}

function renderJobs() {
  const jobs = [...state.jobs].sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt));
  jobCount.textContent = `${jobs.length} total`;
  if (!jobs.length) {
    jobList.innerHTML = `<div class="job-item empty"><div><div class="job-name">No jobs yet</div><div class="job-meta">Start an enhance job to see it here.</div></div></div>`;
    return;
  }

  jobList.innerHTML = jobs.map((job) => `
    <button class="job-item ${job.id === state.currentJobId ? "is-active" : ""}" type="button" data-id="${escapeHTML(job.id)}">
      <div>
        <div class="job-name">${escapeHTML(job.inputName)}</div>
        <div class="job-meta">${escapeHTML(labelForMode(job.mode))} · ${escapeHTML(job.preset)} · ${Math.round(job.progress)}%</div>
      </div>
      <span class="job-badge ${escapeHTML(job.status)}">${escapeHTML(job.status)}</span>
    </button>
  `).join("");

  for (const item of jobList.querySelectorAll(".job-item[data-id]")) {
    item.addEventListener("click", async () => {
      state.currentJobId = item.dataset.id;
      const job = state.jobs.find((candidate) => candidate.id === state.currentJobId);
      if (job) renderJob(job);
      renderJobs();
    });
  }
}

function renderJob(job) {
  if (!job) return;
  state.currentJobId = job.id;
  currentJobMeta.textContent = `${labelForMode(job.mode)} · ${job.preset} · ${job.status}`;
  stageText.textContent = job.stage || "Working";
  const progress = Math.max(0, Math.min(100, Number(job.progress) || 0));
  progressText.textContent = `${Math.round(progress)}%`;
  progressFill.style.width = `${progress}%`;
  outputPath.textContent = job.outputPath || "-";

  const running = job.status === "queued" || job.status === "running";
  cancelButton.disabled = !running;
  progressFill.classList.toggle("is-active", running);

  if (job.status === "succeeded") {
    downloadLink.href = `/api/jobs/${job.id}/download`;
    downloadLink.classList.remove("is-hidden");
  } else {
    downloadLink.classList.add("is-hidden");
  }

  const logs = job.logs || [];
  logMeta.textContent = logs.length ? `${logs.length} lines` : "No output";
  logOutput.textContent = logs.map((line) => {
    const time = new Date(line.time);
    const stamp = Number.isNaN(time.getTime()) ? "--:--:--" : time.toLocaleTimeString();
    return `[${stamp}] ${line.message}`;
  }).join("\n");
  logOutput.scrollTop = logOutput.scrollHeight;
}

async function parseJSON(response) {
  const text = await response.text();
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch {
    return { error: text };
  }
}

function labelForMode(mode) {
  const labels = {
    fast: "Clean",
    "fast-upscale": "Clean 2x",
    "ai-2x": "AI 2x",
    "ai-4x": "AI 4x",
    anime: "Anime"
  };
  return labels[mode] || mode;
}

function updateResourceHint() {
  const mode = form.elements.mode.value;
  const preset = form.elements.preset.value;
  const hints = {
    fast: {
      title: "Clean",
      level: "CPU high",
      summary: "FFmpeg denoise, sharpen, color, and encode. Fastest path, no AI frame upscaling.",
      cpu: "High; FFmpeg may use most CPU threads",
      gpu: "Low; macOS media encoder may help",
      disk: "Low; no frame directory"
    },
    "fast-upscale": {
      title: "Clean 2x",
      level: "CPU high",
      summary: "FFmpeg cleanup plus Lanczos 2x scale. Fast enough for Shorts, but not AI restoration.",
      cpu: "High; scaling and filters use CPU",
      gpu: "Low to medium; encoder may use media engine",
      disk: "Low; no extracted PNG frames"
    },
    "ai-2x": {
      title: "AI 2x",
      level: "GPU high",
      summary: "Extracts every frame, upscales through Real-ESRGAN on one Vulkan GPU, then rebuilds video.",
      cpu: "Medium; frame extract/rebuild plus still I/O",
      gpu: "High; one Vulkan GPU by default",
      disk: "High; temp stills (JPEG on fast/balanced, PNG on best)"
    },
    "ai-4x": {
      title: "AI 4x",
      level: "GPU very high",
      summary: "Highest output size. Use for short clips only unless you can wait and have disk space.",
      cpu: "Medium to high; many large frames",
      gpu: "Very high; more VRAM and time",
      disk: "Very high; largest temp/output files"
    },
    anime: {
      title: "Anime",
      level: "GPU high",
      summary: "AI 2x path with the anime model. Best for animation, cartoons, UI, and line art.",
      cpu: "Medium; frame extract/rebuild plus still I/O",
      gpu: "High; one Vulkan GPU by default",
      disk: "High; temp stills (JPEG on fast/balanced, PNG on best)"
    }
  };
  const hint = hints[mode] || hints.fast;
  const presetNote = {
    fast: "Fast preset favors speed: lighter filters, higher AI thread fan-out, JPEG intermediates.",
    balanced: "Balanced is recommended: high-quality JPEG intermediates, solid encode quality.",
    best: "Best keeps lossless PNG intermediates and stricter encode. Real-ESRGAN TTA stays off unless REALESRGAN_TTA=1."
  }[preset] || "";

  const levelKey = {
    "CPU high": "medium",
    "GPU high": "high",
    "GPU very high": "very-high"
  }[hint.level] || "medium";

  resourceTitle.textContent = `Resource use: ${hint.title}`;
  resourceLevel.textContent = hint.level;
  resourceSummary.textContent = `${hint.summary} ${presetNote}`;
  resourceCPU.textContent = hint.cpu;
  resourceGPU.textContent = hint.gpu;
  resourceDisk.textContent = hint.disk;

  const resourceHint = document.querySelector("#resourceHint");
  if (resourceHint) resourceHint.dataset.level = levelKey;
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes)) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(value >= 10 || index === 0 ? 0 : 1)} ${units[index]}`;
}

function showToast(message) {
  toast.textContent = message;
  toast.classList.add("is-visible");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => {
    toast.classList.remove("is-visible");
  }, 4200);
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
