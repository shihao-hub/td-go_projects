document.addEventListener("DOMContentLoaded", () => {
  // 状态变量
  let currentResults = [];
  let currentFetchData = null;
  let appConfig = null;
  let isSearching = false;
  let isFetching = false;

  // DOM 元素引用
  const channelBadge = document.getElementById("channelBadge");
  const apiKeyIndicator = document.getElementById("apiKeyIndicator");
  const openSettingsBtn = document.getElementById("openSettingsBtn");
  const closeSettingsBtn = document.getElementById("closeSettingsBtn");
  const settingsModal = document.getElementById("settingsModal");

  // 设置表单元素
  const cfgApiKey = document.getElementById("cfgApiKey");
  const togglePwdBtn = document.getElementById("togglePwdBtn");
  const cfgBaseUrl = document.getElementById("cfgBaseUrl");
  const cfgDefaultCount = document.getElementById("cfgDefaultCount");
  const cfgNeedContent = document.getElementById("cfgNeedContent");
  const testFeedback = document.getElementById("testFeedback");
  const testAuthBtn = document.getElementById("testAuthBtn");
  const saveConfigBtn = document.getElementById("saveConfigBtn");

  // 搜索面板元素
  const searchForm = document.getElementById("searchForm");
  const searchInput = document.getElementById("searchInput");
  const searchSubmitBtn = document.getElementById("searchSubmitBtn");
  const toggleOptionsBtn = document.getElementById("toggleOptionsBtn");
  const optionsPanel = document.getElementById("optionsPanel");
  const searchCount = document.getElementById("searchCount");
  const oriQuestion = document.getElementById("oriQuestion");
  const needContent = document.getElementById("needContent");
  const resultsSection = document.getElementById("resultsSection");
  const resultsHeader = document.getElementById("resultsHeader");
  const resultsCount = document.getElementById("resultsCount");
  const resultsList = document.getElementById("resultsList");
  const exportMarkdownBtn = document.getElementById("exportMarkdownBtn");
  const exportJsonBtn = document.getElementById("exportJsonBtn");

  // Fetch 面板元素
  const fetchUrlInput = document.getElementById("fetchUrlInput");
  const fetchSubmitBtn = document.getElementById("fetchSubmitBtn");
  const fetchMetaBar = document.getElementById("fetchMetaBar");
  const fetchTitle = document.getElementById("fetchTitle");
  const fetchSourceLink = document.getElementById("fetchSourceLink");
  const fetchContentArea = document.getElementById("fetchContentArea");
  const copyMarkdownBtn = document.getElementById("copyMarkdownBtn");
  const saveMarkdownBtn = document.getElementById("saveMarkdownBtn");
  const viewBtns = document.querySelectorAll(".view-btn");

  // 历史面板元素
  const searchHistoryList = document.getElementById("searchHistoryList");
  const fetchHistoryList = document.getElementById("fetchHistoryList");

  // Tab 切换逻辑
  const navTabs = document.querySelectorAll(".nav-tab");
  const tabPanels = document.querySelectorAll(".tab-panel");

  function switchTab(targetTabId) {
    navTabs.forEach(tab => {
      tab.classList.toggle("active", tab.dataset.tab === targetTabId);
    });
    tabPanels.forEach(panel => {
      panel.classList.toggle("active", panel.id === targetTabId);
    });
    if (targetTabId === "historyTab") {
      loadHistory();
    }
  }

  navTabs.forEach(tab => {
    tab.addEventListener("click", () => switchTab(tab.dataset.tab));
  });

  // 展开/收起高级搜索
  toggleOptionsBtn.addEventListener("click", () => {
    toggleOptionsBtn.classList.toggle("open");
    optionsPanel.classList.toggle("open");
  });

  // 设置弹窗打开/关闭
  function openSettings() {
    settingsModal.classList.add("open");
    testFeedback.style.display = "none";
    testFeedback.className = "test-feedback";
  }

  function closeSettings() {
    settingsModal.classList.remove("open");
  }

  openSettingsBtn.addEventListener("click", openSettings);
  apiKeyIndicator.addEventListener("click", openSettings);
  closeSettingsBtn.addEventListener("click", closeSettings);
  settingsModal.addEventListener("click", (e) => {
    if (e.target === settingsModal) closeSettings();
  });

  togglePwdBtn.addEventListener("click", () => {
    if (cfgApiKey.type === "password") {
      cfgApiKey.type = "text";
      togglePwdBtn.textContent = "隐藏";
    } else {
      cfgApiKey.type = "password";
      togglePwdBtn.textContent = "显示";
    }
  });

  // 载入元数据与配置
  async function loadMetaAndConfig() {
    try {
      const metaRes = await fetch("/api/meta");
      if (metaRes.ok) {
        const meta = await metaRes.json();
        channelBadge.textContent = meta.channel || "dev";
        if (meta.channel === "release") {
          channelBadge.style.color = "var(--accent-green)";
        }
      }

      const cfgRes = await fetch("/api/config");
      if (cfgRes.ok) {
        appConfig = await cfgRes.json();
        updateConfigUI(appConfig);
      }
    } catch (err) {
      console.error("加载元数据或配置失败:", err);
    }
  }

  function updateConfigUI(cfg) {
    if (!cfg) return;
    cfgApiKey.value = cfg.apiKey || "";
    cfgBaseUrl.value = cfg.baseUrl || "https://api.deepseek.com";
    cfgDefaultCount.value = cfg.defaultCount || 10;
    cfgNeedContent.checked = !!cfg.needContent;

    searchCount.value = cfg.defaultCount || 10;
    needContent.checked = !!cfg.needContent;

    const hasKey = !!(cfg.apiKey && cfg.apiKey.trim().length > 0);
    apiKeyIndicator.className = "api-key-indicator " + (hasKey ? "configured" : "unconfigured");
    apiKeyIndicator.querySelector(".status-text").textContent = hasKey ? "API Key 已配置" : "API Key 未配置";
  }

  // 保存配置
  saveConfigBtn.addEventListener("click", async () => {
    const payload = {
      apiKey: cfgApiKey.value.trim(),
      baseUrl: cfgBaseUrl.value.trim(),
      defaultCount: parseInt(cfgDefaultCount.value, 10) || 10,
      needContent: cfgNeedContent.checked,
    };

    saveConfigBtn.disabled = true;
    saveConfigBtn.textContent = "保存中...";
    try {
      const res = await fetch("/api/config", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "保存失败");

      appConfig = payload;
      updateConfigUI(appConfig);
      testFeedback.textContent = "配置保存成功！";
      testFeedback.className = "test-feedback success";
      setTimeout(closeSettings, 1000);
    } catch (err) {
      testFeedback.textContent = err.message;
      testFeedback.className = "test-feedback error";
    } finally {
      saveConfigBtn.disabled = false;
      saveConfigBtn.textContent = "保存配置";
    }
  });

  // 测试连通性（优先使用完全免费的 GET /models 探测端点）
  testAuthBtn.addEventListener("click", async () => {
    const payload = {
      apiKey: cfgApiKey.value.trim(),
      baseUrl: cfgBaseUrl.value.trim(),
    };
    if (!payload.apiKey) {
      testFeedback.textContent = "请先输入 API Key";
      testFeedback.className = "test-feedback error";
      return;
    }

    testAuthBtn.disabled = true;
    testAuthBtn.textContent = "测试中...";
    testFeedback.textContent = "正在向 DeepSeek 发起免费端点探测...";
    testFeedback.className = "test-feedback";
    testFeedback.style.display = "block";

    try {
      const res = await fetch("/api/config/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "测试失败");

      testFeedback.textContent = "连通性测试通过！(免费探针验证成功，0 元消耗)";
      testFeedback.className = "test-feedback success";
    } catch (err) {
      testFeedback.textContent = "测试未通过: " + err.message;
      testFeedback.className = "test-feedback error";
    } finally {
      testAuthBtn.disabled = false;
      testAuthBtn.textContent = "连通性测试";
    }
  });

  // 执行搜索
  searchForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    if (isSearching) return;

    const query = searchInput.value.trim();
    if (!query) return;

    if (!appConfig || !appConfig.apiKey) {
      openSettings();
      testFeedback.textContent = "开始搜索前请先配置 DeepSeek API Key";
      testFeedback.className = "test-feedback error";
      return;
    }

    isSearching = true;
    searchSubmitBtn.disabled = true;
    searchSubmitBtn.textContent = "检索中...";
    resultsList.innerHTML = `<div class="empty-state"><p>正在调用 DeepSeek Search 引擎检索中...</p></div>`;

    try {
      const res = await fetch("/api/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: query,
          count: parseInt(searchCount.value, 10) || 10,
          ori_question: oriQuestion.value.trim(),
          need_content: needContent.checked,
          refresh: false,
        }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "搜索失败");

      currentResults = data.results || [];
      renderSearchResults(currentResults, data.from_cache, data.cached_at);
    } catch (err) {
      resultsList.innerHTML = `
        <div class="empty-state">
          <p style="color: var(--accent-red)">检索失败: ${escapeHtml(err.message)}</p>
          <small class="text-muted">请检查 API Key 是否有效或网络连接</small>
        </div>
      `;
    } finally {
      isSearching = false;
      searchSubmitBtn.disabled = false;
      searchSubmitBtn.textContent = "搜索";
    }
  });

  // 渲染搜索结果
  function renderSearchResults(results, fromCache, cachedAt) {
    if (!results || results.length === 0) {
      resultsHeader.style.display = "none";
      resultsList.innerHTML = `
        <div class="empty-state">
          <p>未找到符合条件的网页结果</p>
          <small class="text-muted">可尝试简化搜索词或检查关键词拼写</small>
        </div>
      `;
      return;
    }

    resultsCount.textContent = results.length;
    resultsHeader.style.display = "flex";

    // 缓存徽章处理（容错挂载到 .results-meta）
    let cacheBadge = document.getElementById("searchCacheBadge");
    if (!cacheBadge) {
      cacheBadge = document.createElement("span");
      cacheBadge.id = "searchCacheBadge";
      cacheBadge.className = "badge";
      const statsContainer = resultsHeader.querySelector(".results-meta") || resultsHeader;
      statsContainer.appendChild(cacheBadge);
    }
    if (fromCache) {
      cacheBadge.style.display = "inline-block";
      cacheBadge.style.background = "rgba(46,160,67,0.15)";
      cacheBadge.style.color = "var(--accent-green)";
      cacheBadge.style.border = "1px solid rgba(46,160,67,0.3)";
      cacheBadge.style.marginLeft = "8px";
      cacheBadge.textContent = "本地缓存 (0 元消耗)";
      cacheBadge.title = cachedAt ? `缓存于: ${cachedAt}` : "近期搜索缓存";
    } else {
      cacheBadge.style.display = "none";
    }

    resultsList.innerHTML = "";

    results.forEach((item, index) => {
      const card = document.createElement("div");
      card.className = "result-card";

      const score = item.rerank_score || 0;
      const isHighScore = score >= 1.5;
      const scoreBadgeHtml = `<span class="badge badge-score ${isHighScore ? 'high' : ''}" title="Rerank 相关性与质量评分">相关度 ${score.toFixed(2)}</span>`;
      
      let authorityHtml = "";
      if (item.authority) {
        authorityHtml = `<span class="badge" style="background: rgba(210,153,34,0.15); color: var(--accent-orange); border: 1px solid rgba(210,153,34,0.3);">信源 Lv.${escapeHtml(String(item.authority))}</span>`;
      }

      const hasContent = !!(item.content && item.content.trim().length > 0);

      card.innerHTML = `
        <div class="card-top">
          <div class="card-title-group">
            <h3 class="card-title">
              <a href="${escapeHtml(item.url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(item.title || "无标题网页")}</a>
            </h3>
            <div class="card-url">${escapeHtml(item.url)}</div>
          </div>
          <div class="card-badges">
            ${scoreBadgeHtml}
            ${authorityHtml}
          </div>
        </div>

        <div class="card-body">
          <div class="content-tabs">
            <button class="content-tab-btn active" data-view="snippet">片段 (Snippet)</button>
            <button class="content-tab-btn" data-view="summary">LLM摘要 (Summary)</button>
            ${hasContent ? `<button class="content-tab-btn" data-view="content">抓取正文</button>` : ''}
          </div>
          <div class="tab-content snippet-view">
            <p class="snippet-text">${escapeHtml(item.snippet || "暂无简短描述")}</p>
          </div>
          <div class="tab-content summary-view" style="display: none;">
            <div class="summary-text">${escapeHtml(item.summary || "暂无长摘要")}</div>
          </div>
          ${hasContent ? `
            <div class="tab-content content-view" style="display: none;">
              <pre class="markdown-raw" style="max-height: 240px; overflow-y: auto;">${escapeHtml(item.content)}</pre>
            </div>
          ` : ''}
        </div>

        <div class="card-footer">
          <button class="btn btn-sm btn-outline copy-url-btn" data-url="${escapeHtml(item.url)}">复制链接</button>
          <button class="btn btn-sm btn-outline copy-md-btn" data-index="${index}">复制引用</button>
          <button class="btn btn-sm btn-primary jump-fetch-btn" data-url="${escapeHtml(item.url)}">${hasContent ? '直读正文 (0 元)' : '提取为 Markdown'}</button>
        </div>
      `;

      // 绑定卡片内摘要切换
      const tabBtns = card.querySelectorAll(".content-tab-btn");
      const snippetView = card.querySelector(".snippet-view");
      const summaryView = card.querySelector(".summary-view");
      const contentView = card.querySelector(".content-view");

      tabBtns.forEach(btn => {
        btn.addEventListener("click", () => {
          tabBtns.forEach(b => b.classList.remove("active"));
          btn.classList.add("active");
          const viewType = btn.dataset.view;

          if (snippetView) snippetView.style.display = viewType === "snippet" ? "block" : "none";
          if (summaryView) summaryView.style.display = viewType === "summary" ? "block" : "none";
          if (contentView) contentView.style.display = viewType === "content" ? "block" : "none";
        });
      });

      // 复制链接
      card.querySelector(".copy-url-btn").addEventListener("click", (e) => {
        navigator.clipboard.writeText(e.target.dataset.url);
        showTempToast(e.target, "已复制");
      });

      // 复制 Markdown 引用
      card.querySelector(".copy-md-btn").addEventListener("click", (e) => {
        const md = `[${item.title || "链接"}](${item.url})`;
        navigator.clipboard.writeText(md);
        showTempToast(e.target, "已复制 Markdown");
      });

      // 跳转提取全文（0 元复用：若已包含 content，直接加载渲染，绝不重复打 API）
      card.querySelector(".jump-fetch-btn").addEventListener("click", (e) => {
        const targetUrl = e.target.dataset.url;
        fetchUrlInput.value = targetUrl;
        switchTab("fetchTab");

        if (hasContent) {
          currentFetchData = {
            url: targetUrl,
            title: item.title || targetUrl,
            content: item.content,
          };
          renderFetchResult(currentFetchData, true);
          showTempToast(copyMarkdownBtn, "已从搜索结果直接加载正文 (0 元消耗)");
          return;
        }

        doFetch(targetUrl);
      });

      resultsList.appendChild(card);
    });
  }

  // 导出功能
  exportMarkdownBtn.addEventListener("click", () => {
    if (!currentResults || currentResults.length === 0) return;
    const lines = currentResults.map(item => `- [${item.title}](${item.url}) (得分: ${item.rerank_score})\n  > ${item.snippet}`);
    const md = lines.join("\n\n");
    navigator.clipboard.writeText(md);
    showTempToast(exportMarkdownBtn, "已复制 Markdown 列表");
  });

  exportJsonBtn.addEventListener("click", () => {
    if (!currentResults || currentResults.length === 0) return;
    const jsonStr = JSON.stringify(currentResults, null, 2);
    downloadFile("search_results.json", jsonStr, "application/json");
  });

  // Fetch 提取逻辑
  fetchSubmitBtn.addEventListener("click", () => {
    if (isFetching) return;
    const url = fetchUrlInput.value.trim();
    if (!url) return;
    doFetch(url);
  });

  fetchUrlInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      if (isFetching) return;
      const url = fetchUrlInput.value.trim();
      if (url) doFetch(url);
    }
  });

  async function doFetch(url, refresh = false) {
    if (isFetching) return;

    if (!appConfig || !appConfig.apiKey) {
      openSettings();
      testFeedback.textContent = "提取网页前请先配置 DeepSeek API Key";
      testFeedback.className = "test-feedback error";
      return;
    }

    isFetching = true;
    fetchSubmitBtn.disabled = true;
    fetchSubmitBtn.textContent = "正在抓取与解析...";
    fetchContentArea.innerHTML = `<div class="empty-state"><p>正在加载并解析网页正文 Markdown...</p></div>`;

    try {
      const res = await fetch("/api/fetch", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: url, refresh: refresh }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "抓取失败");

      currentFetchData = {
        url: url,
        title: data.title || url,
        content: data.content || "",
      };
      renderFetchResult(currentFetchData, data.from_cache);
    } catch (err) {
      fetchContentArea.innerHTML = `
        <div class="empty-state">
          <p style="color: var(--accent-red)">网页抓取失败: ${escapeHtml(err.message)}</p>
          <small class="text-muted">请确认目标 URL 可正常访问且 DeepSeek 服务正常</small>
        </div>
      `;
      fetchMetaBar.style.display = "none";
    } finally {
      isFetching = false;
      fetchSubmitBtn.disabled = false;
      fetchSubmitBtn.textContent = "提取正文";
    }
  }

  function renderFetchResult(data, fromCache) {
    fetchTitle.textContent = data.title;
    fetchSourceLink.href = data.url;
    fetchMetaBar.style.display = "flex";

    let fetchCacheBadge = document.getElementById("fetchCacheBadge");
    if (!fetchCacheBadge) {
      fetchCacheBadge = document.createElement("span");
      fetchCacheBadge.id = "fetchCacheBadge";
      fetchCacheBadge.className = "badge";
      const fetchTitleContainer = fetchMetaBar.querySelector(".fetch-title-wrapper") || fetchMetaBar;
      fetchTitleContainer.appendChild(fetchCacheBadge);
    }
    if (fromCache) {
      fetchCacheBadge.style.display = "inline-block";
      fetchCacheBadge.style.background = "rgba(46,160,67,0.15)";
      fetchCacheBadge.style.color = "var(--accent-green)";
      fetchCacheBadge.style.border = "1px solid rgba(46,160,67,0.3)";
      fetchCacheBadge.style.marginLeft = "8px";
      fetchCacheBadge.textContent = "本地缓存 (0 元消耗)";
    } else {
      fetchCacheBadge.style.display = "none";
    }

    updateFetchView("rendered");
  }

  function updateFetchView(viewMode) {
    viewBtns.forEach(btn => {
      btn.classList.toggle("active", btn.dataset.view === viewMode);
    });

    if (!currentFetchData) return;

    if (viewMode === "rendered") {
      fetchContentArea.innerHTML = `
        <div class="markdown-rendered">
          ${renderSimpleMarkdown(currentFetchData.content)}
        </div>
      `;
    } else {
      fetchContentArea.innerHTML = `
        <pre class="markdown-raw">${escapeHtml(currentFetchData.content)}</pre>
      `;
    }
  }

  viewBtns.forEach(btn => {
    btn.addEventListener("click", () => updateFetchView(btn.dataset.view));
  });

  copyMarkdownBtn.addEventListener("click", () => {
    if (!currentFetchData) return;
    navigator.clipboard.writeText(currentFetchData.content);
    showTempToast(copyMarkdownBtn, "已复制 Markdown");
  });

  saveMarkdownBtn.addEventListener("click", () => {
    if (!currentFetchData) return;
    const safeTitle = (currentFetchData.title || "article").replace(/[\/\\:*?"<>|]/g, "_");
    downloadFile(`${safeTitle}.md`, currentFetchData.content, "text/markdown");
  });

  // 加载历史
  async function loadHistory() {
    try {
      const sRes = await fetch("/api/history/searches");
      if (sRes.ok) {
        const searches = await sRes.json();
        renderSearchHistory(searches);
      }

      const fRes = await fetch("/api/history/fetches");
      if (fRes.ok) {
        const fetches = await fRes.json();
        renderFetchHistory(fetches);
      }
    } catch (err) {
      console.error("加载历史记录失败:", err);
    }
  }

  function renderSearchHistory(items) {
    if (!items || items.length === 0) {
      searchHistoryList.innerHTML = `<li class="history-empty">暂无搜索历史</li>`;
      return;
    }
    searchHistoryList.innerHTML = "";
    items.forEach(item => {
      const li = document.createElement("li");
      li.className = "history-item";
      li.innerHTML = `
        <span class="history-item-title">${escapeHtml(item.query)}</span>
        <span class="history-item-meta">${item.resultCount} 条结果</span>
      `;
      li.addEventListener("click", () => {
        searchInput.value = item.query;
        switchTab("searchTab");
        searchForm.dispatchEvent(new Event("submit"));
      });
      searchHistoryList.appendChild(li);
    });
  }

  function renderFetchHistory(items) {
    if (!items || items.length === 0) {
      fetchHistoryList.innerHTML = `<li class="history-empty">暂无抓取记录</li>`;
      return;
    }
    fetchHistoryList.innerHTML = "";
    items.forEach(item => {
      const li = document.createElement("li");
      li.className = "history-item";
      li.innerHTML = `
        <span class="history-item-title">${escapeHtml(item.title || item.url)}</span>
        <span class="history-item-meta">网页提取</span>
      `;
      li.addEventListener("click", () => {
        fetchUrlInput.value = item.url;
        switchTab("fetchTab");
        currentFetchData = {
          url: item.url,
          title: item.title || item.url,
          content: item.content || "",
        };
        renderFetchResult(currentFetchData);
      });
      fetchHistoryList.appendChild(li);
    });
  }

  // 工具函数
  function escapeHtml(text) {
    if (!text) return "";
    return String(text)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#039;");
  }

  function showTempToast(el, text) {
    const orig = el.textContent;
    el.textContent = text;
    setTimeout(() => { el.textContent = orig; }, 1500);
  }

  function downloadFile(filename, content, type) {
    const blob = new Blob([content], { type: type });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  // 极简纯净 Markdown 渲染器（无需外部笨重脚本）
  function renderSimpleMarkdown(md) {
    if (!md) return "<p>无内容</p>";

    const lines = md.split("\n");
    let html = "";
    let inCodeBlock = false;
    let codeBuffer = [];

    for (let i = 0; i < lines.length; i++) {
      let line = lines[i];

      if (line.trim().startsWith("```")) {
        if (!inCodeBlock) {
          inCodeBlock = true;
          codeBuffer = [];
        } else {
          inCodeBlock = false;
          html += `<pre><code>${escapeHtml(codeBuffer.join("\n"))}</code></pre>`;
        }
        continue;
      }

      if (inCodeBlock) {
        codeBuffer.push(line);
        continue;
      }

      // 标题
      if (line.startsWith("# ")) {
        html += `<h1>${formatInline(line.slice(2))}</h1>`;
      } else if (line.startsWith("## ")) {
        html += `<h2>${formatInline(line.slice(3))}</h2>`;
      } else if (line.startsWith("### ")) {
        html += `<h3>${formatInline(line.slice(4))}</h3>`;
      } else if (line.startsWith("#### ")) {
        html += `<h4>${formatInline(line.slice(5))}</h4>`;
      } else if (line.trim().startsWith("- ") || line.trim().startsWith("* ")) {
        html += `<ul><li>${formatInline(line.trim().slice(2))}</li></ul>`;
      } else if (/^\d+\.\s/.test(line.trim())) {
        const text = line.trim().replace(/^\d+\.\s/, "");
        html += `<ol><li>${formatInline(text)}</li></ol>`;
      } else if (line.trim().startsWith("> ")) {
        html += `<blockquote><p>${formatInline(line.trim().slice(2))}</p></blockquote>`;
      } else if (line.trim().length === 0) {
        html += "<br/>";
      } else {
        html += `<p>${formatInline(line)}</p>`;
      }
    }

    // 规整相邻 ul/ol
    html = html.replace(/<\/ul>\s*<ul>/g, "").replace(/<\/ol>\s*<ol>/g, "");
    return html;
  }

  function formatInline(str) {
    let s = escapeHtml(str);
    // 粗体
    s = s.replace(/\*\*(.*?)\*\*/g, "<strong>$1</strong>");
    // 行内代码
    s = s.replace(/`([^`]+)`/g, "<code>$1</code>");
    // 链接
    s = s.replace(/\[(.*?)\]\((.*?)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>');
    return s;
  }

  // 页面启动初始化
  loadMetaAndConfig();
});
