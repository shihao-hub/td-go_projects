// Anatomy Manager WebUI Engine
(function () {
  let artworks = [];
  let currentArtwork = null;
  let currentVariant = "dark"; // dark or light
  let activeTab = "render";

  // Elements
  const artworkListEl = document.getElementById("artworkList");
  const artCountEl = document.getElementById("artCount");
  const searchInput = document.getElementById("searchInput");
  const themeToggleBtn = document.getElementById("themeToggleBtn");
  const refreshBtn = document.getElementById("refreshBtn");

  const viewerToolbar = document.getElementById("viewerToolbar");
  const viewTabs = document.getElementById("viewTabs");
  const artTitle = document.getElementById("artTitle");
  const artCategory = document.getElementById("artCategory");
  const artSession = document.getElementById("artSession");
  const btnVariantDark = document.getElementById("btnVariantDark");
  const btnVariantLight = document.getElementById("btnVariantLight");
  const btnNewTab = document.getElementById("btnNewTab");

  const iframeWrapper = document.getElementById("iframeWrapper");
  const docContent = document.getElementById("docContent");
  const sourceCode = document.getElementById("sourceCode");
  const filesTableBody = document.getElementById("filesTableBody");

  // Init
  window.addEventListener("DOMContentLoaded", () => {
    bindEvents();
    loadArtworks();
  });

  function bindEvents() {
    refreshBtn.addEventListener("click", loadArtworks);
    searchInput.addEventListener("input", filterArtworks);

    themeToggleBtn.addEventListener("click", () => {
      document.body.classList.toggle("theme-light");
    });

    // Variant selector
    btnVariantDark.addEventListener("click", () => setVariant("dark"));
    btnVariantLight.addEventListener("click", () => setVariant("light"));

    // Tab buttons
    document.querySelectorAll(".tab-btn").forEach((btn) => {
      btn.addEventListener("click", (e) => {
        const tab = e.target.dataset.tab;
        switchTab(tab);
      });
    });

    btnNewTab.addEventListener("click", () => {
      if (!currentArtwork) return;
      const targetFile = getActiveRenderFile();
      window.open(`/api/artworks/${currentArtwork.id}/files/${targetFile}`, "_blank");
    });
  }

  async function loadArtworks() {
    try {
      const res = await fetch("/api/artworks");
      if (!res.ok) throw new Error("加载图谱列表失败");
      artworks = await res.json();
      renderList(artworks);

      if (artworks && artworks.length > 0 && !currentArtwork) {
        selectArtwork(artworks[0].id);
      }
    } catch (err) {
      console.error(err);
      artworkListEl.innerHTML = `<div class="empty-hint" style="color:#ef4444;">连接后端失败: ${err.message}</div>`;
    }
  }

  function renderList(items) {
    artCountEl.textContent = `${items.length} 个图谱`;
    if (!items || items.length === 0) {
      artworkListEl.innerHTML = `<div class="empty-hint">暂无图谱，可通过 CLI 摄取归档...</div>`;
      return;
    }

    artworkListEl.innerHTML = items
      .map((item) => {
        const activeClass = currentArtwork && currentArtwork.id === item.id ? "active" : "";
        // Find thumbnail if exists
        const thumbFile = (item.files || []).find((f) => f.endsWith(".png") && !f.includes("light")) ||
                          (item.files || []).find((f) => f.endsWith(".png")) || "";
        const thumbUrl = thumbFile ? `/api/artworks/${item.id}/files/${thumbFile}` : "";

        return `
          <div class="art-card ${activeClass}" data-id="${item.id}">
            ${thumbUrl ? `
              <div class="art-card-thumb">
                <img src="${thumbUrl}" alt="${item.title}" loading="lazy" />
              </div>
            ` : ""}
            <div class="art-card-title">${escapeHtml(item.title || item.id)}</div>
            <div class="art-card-desc">${escapeHtml(item.description || "基于物理机械隐喻的交互解构")}</div>
            <div class="art-card-meta">
              <span>🏷️ ${escapeHtml(item.category || "figure")}</span>
              <span>${formatDate(item.updatedAt)}</span>
            </div>
          </div>
        `;
      })
      .join("");

    // Bind card clicks
    document.querySelectorAll(".art-card").forEach((card) => {
      card.addEventListener("click", () => {
        selectArtwork(card.dataset.id);
      });
    });
  }

  function filterArtworks() {
    const q = searchInput.value.trim().toLowerCase();
    if (!q) {
      renderList(artworks);
      return;
    }
    const filtered = artworks.filter(
      (a) =>
        a.id.toLowerCase().includes(q) ||
        (a.title && a.title.toLowerCase().includes(q)) ||
        (a.description && a.description.toLowerCase().includes(q)) ||
        (a.tags && a.tags.some((t) => t.toLowerCase().includes(q)))
    );
    renderList(filtered);
  }

  async function selectArtwork(id) {
    try {
      const res = await fetch(`/api/artworks/${id}`);
      if (!res.ok) throw new Error("获取详情失败");
      currentArtwork = await res.json();

      // Update UI cards active state
      document.querySelectorAll(".art-card").forEach((card) => {
        card.classList.toggle("active", card.dataset.id === id);
      });

      // Show toolbars
      viewerToolbar.style.display = "flex";
      viewTabs.style.display = "flex";

      artTitle.textContent = currentArtwork.title || currentArtwork.id;
      artCategory.textContent = currentArtwork.category || "figure";
      artSession.textContent = currentArtwork.sessionId ? `Session: ${currentArtwork.sessionId.slice(0, 8)}...` : "";

      // Load render iframe
      loadRenderFrame();

      // Load doc
      loadDoc();

      // Load source
      loadSource();

      // Load files list
      loadFilesTable();
    } catch (err) {
      alert("加载图谱详情出错: " + err.message);
    }
  }

  function getActiveRenderFile() {
    if (!currentArtwork || !currentArtwork.files) return "";
    const files = currentArtwork.files;
    if (currentVariant === "light") {
      const lightHtml = files.find((f) => f.includes("light") && f.endsWith(".html"));
      if (lightHtml) return lightHtml;
    }
    const darkHtml = files.find((f) => !f.includes("light") && f.endsWith(".html"));
    if (darkHtml) return darkHtml;
    return files.find((f) => f.endsWith(".html")) || "";
  }

  function loadRenderFrame() {
    const targetFile = getActiveRenderFile();
    if (!targetFile) {
      iframeWrapper.innerHTML = `<div class="placeholder-msg">该作品暂无可渲染的 HTML 文件</div>`;
      return;
    }
    const url = `/api/artworks/${currentArtwork.id}/files/${targetFile}`;
    iframeWrapper.innerHTML = `<iframe src="${url}" sandbox="allow-scripts allow-same-origin"></iframe>`;
  }

  function setVariant(variant) {
    currentVariant = variant;
    btnVariantDark.classList.toggle("active", variant === "dark");
    btnVariantLight.classList.toggle("active", variant === "light");
    loadRenderFrame();
  }

  function switchTab(tab) {
    activeTab = tab;
    document.querySelectorAll(".tab-btn").forEach((btn) => {
      btn.classList.toggle("active", btn.dataset.tab === tab);
    });
    document.querySelectorAll(".tab-pane").forEach((pane) => {
      pane.classList.toggle("active", pane.id === `pane-${tab}`);
    });
  }

  async function loadDoc() {
    if (!currentArtwork) return;
    const md = currentArtwork.docMarkdown;
    if (!md) {
      docContent.innerHTML = `<div class="placeholder-msg">该作品未关联原理解析 Markdown 文档</div>`;
      return;
    }
    // Simple markdown renderer for headers, tables and pre
    docContent.innerHTML = renderSimpleMarkdown(md);
  }

  async function loadSource() {
    if (!currentArtwork || !currentArtwork.files) return;
    const buildFile = currentArtwork.files.find((f) => f.endsWith(".mjs") || f.endsWith(".js") || f.endsWith(".ts"));
    if (!buildFile) {
      sourceCode.textContent = "// 未找到生成脚本源码 (build.mjs)";
      return;
    }
    try {
      const res = await fetch(`/api/artworks/${currentArtwork.id}/files/${buildFile}`);
      const text = await res.text();
      sourceCode.textContent = text;
    } catch (e) {
      sourceCode.textContent = `// 加载源码失败: ${e.message}`;
    }
  }

  function loadFilesTable() {
    if (!currentArtwork || !currentArtwork.files) return;
    filesTableBody.innerHTML = currentArtwork.files
      .map((f) => {
        const ext = f.split(".").pop();
        const url = `/api/artworks/${currentArtwork.id}/files/${f}`;
        return `
          <tr>
            <td><strong>${escapeHtml(f)}</strong></td>
            <td><code>${escapeHtml(ext)}</code></td>
            <td>SQLite 归档</td>
            <td>
              <a href="${url}" target="_blank" class="btn" style="padding:2px 8px; font-size:0.75rem;">查看 / 下载</a>
            </td>
          </tr>
        `;
      })
      .join("");
  }

  function renderSimpleMarkdown(md) {
    // Basic text to HTML conversion for instant viewing without heavy external bundle
    let html = escapeHtml(md);
    // Headings
    html = html.replace(/^### (.*$)/gim, "<h3>$1</h3>");
    html = html.replace(/^## (.*$)/gim, "<h2>$1</h2>");
    html = html.replace(/^# (.*$)/gim, "<h1>$1</h1>");
    // Code blocks
    html = html.replace(/```([\s\S]*?)```/gim, "<pre><code>$1</code></pre>");
    // Bold
    html = html.replace(/\*\*(.*)\*\*/gim, "<strong>$1</strong>");
    // Line breaks
    html = html.replace(/\n/gim, "<br>");
    return html;
  }

  function formatDate(dStr) {
    if (!dStr) return "";
    try {
      const d = new Date(dStr);
      return d.toLocaleDateString();
    } catch (e) {
      return dStr;
    }
  }

  function escapeHtml(str) {
    if (!str) return "";
    return str
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }
})();
