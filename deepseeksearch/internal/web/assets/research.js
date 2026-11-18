document.addEventListener("DOMContentLoaded", () => {
  const $ = id => document.getElementById(id);
  const escape = value => String(value ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
  const safeURL = value => { try { const u = new URL(value); return ["https:", "http:"].includes(u.protocol) ? u.href : "#"; } catch { return "#"; } };
  const labels = {pending:"等待", running:"执行中", success:"完成", failed:"失败", partial:"部分完成", interrupted:"中断", too_broad:"太泛", off_topic:"偏题", need_examples:"缺少实际案例", need_counterexamples:"缺少反面材料", custom:"其他反馈"};
  let session = null, busy = false, dirty = false, editVersion = 0, saveTimer, savePromise, controller;

  function notice(text, error = false) { $("researchNotice").textContent = text; $("researchNotice").classList.toggle("is-error", error); }
  async function request(path, method = "GET", data, signal) {
    const response = await fetch("/api/research" + path, {method, headers:{"Content-Type":"application/json"}, body:data === undefined ? undefined : JSON.stringify(data), signal});
    const envelope = await response.json();
    if (!envelope.ok) { const err = new Error(envelope.error?.message || "操作失败，请重试"); err.data = envelope.data; err.code = envelope.error?.code; throw err; }
    return envelope.data;
  }
  function setBusy(value) {
    busy = value;
    $("researchControls").disabled = value || !!session?.busy;
    $("researchFeedbackControls").disabled = value || !!session?.busy;
    $("researchCreate").querySelectorAll("input,button").forEach(el => el.disabled = value);
    $("researchSessions").querySelectorAll("button").forEach(el => el.disabled = value);
    $("researchReload").disabled = value;
  }
  async function loadList() {
    const items = await request("");
    $("researchSessions").innerHTML = items.length ? items.map(item => `<button type="button" class="research-session ${item.id === session?.id ? "selected" : ""}" data-id="${escape(item.id)}"><strong>${escape(item.question)}</strong><small>${escape(new Date(item.updatedAt).toLocaleString())}</small></button>`).join("") : '<p class="research-muted">还没有研究记录。</p>';
    return items;
  }
  function markDirty() {
    dirty = true; editVersion++; $("researchSaveState").textContent = "草稿尚未保存";
    clearTimeout(saveTimer); saveTimer = setTimeout(() => saveDraft().catch(err => notice(err.message, true)), 650);
    updateBudget();
  }
  async function saveDraft() {
    clearTimeout(saveTimer);
    if (savePromise) { await savePromise; if (dirty) return saveDraft(); return; }
    if (!session || !dirty) return;
    const version = editVersion, id = session.id;
    const draft = {...structuredClone(session.draft), revision:session.revision};
    $("researchSaveState").textContent = "保存中…";
    savePromise = request(`/${id}/draft`, "PUT", draft);
    try {
      const saved = await savePromise;
      if (session?.id === id) { session.revision = saved.revision; session.updatedAt = saved.updatedAt; dirty = editVersion !== version; $("researchSaveState").textContent = dirty ? "草稿尚未保存" : "草稿已保存"; }
    } catch (err) { dirty = true; $("researchSaveState").textContent = "保存失败，本地草稿保留"; throw err; }
    finally { savePromise = null; }
    if (dirty) return saveDraft();
  }
  function updateBudget() {
    const count = session?.draft.queries.filter(q => q.selected).length || 0;
    $("researchBudget").textContent = `已选 ${count}/6 路，每路 ${session?.draft.count || 10} 条；最多同时执行 3 路`;
  }
  function renderDraft() {
    $("researchQueries").innerHTML = session.draft.queries.map((q, i) => `<div class="research-query" data-index="${i}"><input type="checkbox" data-field="selected" aria-label="选择第 ${i + 1} 条查询" ${q.selected ? "checked" : ""}><div><input data-field="query" value="${escape(q.query)}" aria-label="搜索词 ${i + 1}" placeholder="搜索词"><input data-field="reason" value="${escape(q.reason)}" aria-label="检索目的 ${i + 1}" placeholder="检索目的（可选）"></div><button type="button" class="research-remove" aria-label="删除第 ${i + 1} 条查询">×</button></div>`).join("") || '<p class="research-muted">添加搜索词，或根据已保存的反馈生成补搜建议。</p>';
    $("researchCount").value = session.draft.count;
    $("researchContent").checked = session.draft.needContent;
    $("researchLocale").value = session.draft.localization;
    updateBudget();
  }
  function renderResults() {
    const selected = $("researchFilter").value;
    const latest = session.rounds.length;
    const results = session.results.filter(r => !selected || r.sources.some(s => s.queryId === selected));
    $("researchResultCount").textContent = `${session.results.length} 个网页 · 本轮新增 ${session.results.filter(r => r.firstRound === latest).length} 个`;
    $("researchResults").innerHTML = results.map(r => `<article class="research-result"><div class="research-result-top"><a href="${escape(safeURL(r.url))}" target="_blank" rel="noopener noreferrer">${escape(r.title || r.url)}</a>${r.firstRound === latest ? '<span class="research-new">本轮新增</span>' : ""}</div><small>${escape(r.url)}</small><p>${escape(r.snippet || r.summary || "暂无摘要")}</p><div class="research-sources">${r.sources.map(s => `<span title="第 ${s.round} 轮 · 原始分值 ${s.score}">${escape(s.query)} · #${s.rank}</span>`).join("")}</div>${r.content ? `<details><summary>查看抓取正文</summary><pre>${escape(r.content)}</pre></details>` : ""}<button type="button" class="btn btn-outline research-result-feedback" data-id="${escape(r.id)}">针对这条结果反馈</button></article>`).join("") || '<p class="research-muted">执行搜索后，这里会合并各路结果并保留来源。</p>';
  }
  function renderProgress() {
    $("researchRounds").innerHTML = session.rounds.map(r => `<details ${r.number === session.rounds.length ? "open" : ""}><summary>第 ${r.number} 轮 · ${labels[r.status] || r.status}</summary>${r.executions.map(e => `<div class="research-route"><span class="route-status ${escape(e.status)}">${labels[e.status] || escape(e.status)}</span><span>${escape(e.query)}</span><small>${e.fromCache ? "缓存 · " : ""}${e.items.length} 条 ${escape(e.error || "")}</small></div>`).join("")}</details>`).join("");
  }
  function render() {
    $("researchBody").hidden = false;
    $("researchTitle").textContent = session.question;
    $("researchSaveState").textContent = "研究已保存";
    renderDraft(); renderProgress();
    const filter = $("researchFilter").value, options = new Map();
    session.rounds.forEach(r => r.executions.forEach(e => options.set(e.id, e.query)));
    $("researchFilter").innerHTML = '<option value="">全部查询</option>' + [...options].map(([id, query]) => `<option value="${escape(id)}">${escape(query)}</option>`).join("");
    if (options.has(filter)) $("researchFilter").value = filter;
    $("researchFeedbackResult").innerHTML = '<option value="">整体结果</option>' + session.results.map(r => `<option value="${escape(r.id)}">${escape(r.title || r.url)}</option>`).join("");
    $("researchFeedbackHistory").innerHTML = session.feedback.slice(-12).reverse().map(f => `<p><strong>${labels[f.kind] || escape(f.kind)}</strong> ${escape(f.text)}${f.resultId ? " · 针对单条结果" : ""}</p>`).join("");
    renderResults(); setBusy(busy);
  }
  async function action(fn, allowBusy = false) {
    if (busy || (!allowBusy && session?.busy)) return;
    setBusy(true);
    try { await saveDraft(); const value = await fn(); if (value) { session = value; dirty = false; render(); } await loadList(); }
    catch (err) { if (err.data && err.code === "partial_failure") {session = err.data; dirty = false; render();} notice(err.message, true); }
    finally { setBusy(false); }
  }
  $("researchCreate").addEventListener("submit", event => {event.preventDefault(); action(async () => {const v = await request("", "POST", {question:$("researchQuestion").value}); $("researchQuestion").value = ""; notice("研究已创建。可以直接编辑搜索词，也可以按需生成建议。");return v;},true);});
  $("researchSessions").addEventListener("click", event => { const button = event.target.closest("button[data-id]"); if (button) action(async () => {notice("已恢复研究记录。");return request(`/${button.dataset.id}`);}, true); });
  $("researchReload").addEventListener("click", () => {if(dirty && !window.confirm("重新加载会丢弃尚未保存的本地草稿，是否继续？"))return;dirty=false;clearTimeout(saveTimer);action(async () => { const items = await loadList(); const id = session?.id || items[0]?.id; return id ? request(`/${id}`) : null; },true);});
  $("researchQueries").addEventListener("input", event => {const row=event.target.closest("[data-index]");if (!row || !event.target.dataset.field) return; const field=event.target.dataset.field;session.draft.queries[Number(row.dataset.index)][field] = field === "selected" ? event.target.checked : event.target.value;markDirty();});
  $("researchQueries").addEventListener("click", event => {const row=event.target.closest("[data-index]");if(row && event.target.closest(".research-remove")){session.draft.queries.splice(Number(row.dataset.index),1);renderDraft();markDirty();}});
  $("researchAdd").addEventListener("click", () => {if(session.draft.queries.length >= 6){notice("每轮最多 6 路，请先删除不需要的词。",true);return;}session.draft.queries.push({id:"",query:"",reason:"",selected:false});renderDraft();markDirty();$("researchQueries").lastElementChild.querySelector('[data-field="query"]').focus();});
  for (const [id, field, checkbox] of [["researchCount","count",false],["researchLocale","localization",false],["researchContent","needContent",true]]) {$(id).addEventListener("input", () => {session.draft[field]=checkbox ? $(id).checked : field === "count" ? Number($(id).value) : $(id).value;markDirty();});}
  $("researchSave").addEventListener("click", () => action(async () => {notice("草稿已保存。");}));
  const suggest = () => action(async () => {notice("正在生成候选搜索词…");const v=await request(`/${session.id}/suggest`,"POST");notice("建议已追加，请编辑并勾选要执行的查询。");return v;});
  $("researchSuggest").addEventListener("click",suggest);$("researchRefine").addEventListener("click",suggest);
  async function run(retryFailed) {
    await action(async () => {
      if (!retryFailed && !session.draft.queries.some(q => q.selected)) throw new Error("请至少勾选一条查询。");
      const id=session.id;controller=new AbortController();$("researchCancel").hidden=false;
      notice("正在执行查询。已完成结果会逐路保存。");
      let polling=false, active=true;
      const timer=setInterval(async () => {if(polling)return;polling=true;try{const current=await request(`/${id}`);if(active && session?.id===id){session=current;renderProgress();renderResults();}}catch{}finally{polling=false;}},1000);
      try { const v=await request(`/${id}/run`,"POST",{retryFailed,refresh:$("researchRefresh").checked},controller.signal);notice("本轮搜索完成。你可以保存反馈，再生成补搜建议。");return v; }
      catch(err){if(err.name==="AbortError"){notice("已请求停止，正在保存已完成的结果。");let current;for(let attempt=0;attempt<20;attempt++){await new Promise(resolve=>setTimeout(resolve,500));current=await request(`/${id}`);if(!current.busy){notice("本轮已停止，已完成结果已保留。");return current;}}notice("停止处理中，稍后可刷新记录查看保存状态。");return current;}throw err;}
      finally{active=false;clearInterval(timer);controller=null;$("researchCancel").hidden=true;}
    });
  }
  $("researchRun").addEventListener("click",()=>run(false));$("researchRetry").addEventListener("click",()=>run(true));
  $("researchCancel").addEventListener("click",()=>controller?.abort());
  $("researchFeedbackForm").addEventListener("submit",event=>{event.preventDefault();action(async()=>{const v=await request(`/${session.id}/feedback`,"POST",{kind:$("researchFeedbackKind").value,text:$("researchFeedbackText").value,resultId:$("researchFeedbackResult").value});$("researchFeedbackText").value="";notice("反馈已保存，点击生成补搜建议继续。");return v;});});
  $("researchResults").addEventListener("click",event=>{const button=event.target.closest(".research-result-feedback");if(button){$("researchFeedbackResult").value=button.dataset.id;$("researchFeedbackText").focus();}});
  $("researchFilter").addEventListener("change",renderResults);
  $("researchExport").addEventListener("click",()=>{if(!session)return;const url=URL.createObjectURL(new Blob([JSON.stringify(session,null,2)],{type:"application/json"}));const a=document.createElement("a");a.href=url;a.download=`research-${session.id}.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);});
  window.addEventListener("beforeunload",event=>{if(dirty || savePromise){event.preventDefault();event.returnValue="";}});
  loadList().then(async items=>{if(items.length){session=await request(`/${items[0].id}`);render();}}).catch(err=>notice(err.message,true));
});
