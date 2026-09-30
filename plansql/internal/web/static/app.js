let currentItems = [];

async function fetchJSON(url, options = {}) {
    const res = await fetch(url, options);
    const data = await res.json();
    if (!data.ok) {
        throw new Error(data.error || '请求失败');
    }
    return data.data;
}

async function loadData() {
    try {
        const items = await fetchJSON('/api/v1/items');
        currentItems = items || [];
        renderBoard(currentItems);
        updateStats(currentItems);
    } catch (err) {
        alert('加载列表失败: ' + err.message);
    }
}

function parseState(statusStr) {
    try {
        const obj = JSON.parse(statusStr);
        return obj.state || obj.status || 'pending';
    } catch {
        return 'pending';
    }
}

function renderBoard(items) {
    const pendingList = document.getElementById('cards-pending');
    const progressList = document.getElementById('cards-progress');
    const completedList = document.getElementById('cards-completed');

    pendingList.innerHTML = '';
    progressList.innerHTML = '';
    completedList.innerHTML = '';

    let pendingCount = 0;
    let progressCount = 0;
    let completedCount = 0;

    items.forEach(item => {
        const state = parseState(item.status);
        const card = createCardElement(item, state);

        if (state === 'completed') {
            completedList.appendChild(card);
            completedCount++;
        } else if (state === 'in_progress') {
            progressList.appendChild(card);
            progressCount++;
        } else {
            pendingList.appendChild(card);
            pendingCount++;
        }
    });

    document.getElementById('badge-pending').innerText = pendingCount;
    document.getElementById('badge-progress').innerText = progressCount;
    document.getElementById('badge-completed').innerText = completedCount;
}

function createCardElement(item, state) {
    const div = document.createElement('div');
    div.className = 'card';
    div.innerHTML = `
        <span class="card-type type-${item.type}">${item.type}</span>
        <div class="card-path">${escapeHTML(item.path)}</div>
        <div class="card-footer">
            <span>更新于: ${item.updated_at.split(' ')[0]}</span>
            <span>${state}</span>
        </div>
    `;
    div.onclick = () => openEditModal(item);
    return div;
}

function updateStats(items) {
    document.getElementById('stat-total').innerText = items.length;
    let pending = 0, progress = 0, completed = 0;
    items.forEach(it => {
        const st = parseState(it.status);
        if (st === 'completed') completed++;
        else if (st === 'in_progress') progress++;
        else pending++;
    });
    document.getElementById('stat-pending').innerText = pending;
    document.getElementById('stat-progress').innerText = progress;
    document.getElementById('stat-completed').innerText = completed;
}

function escapeHTML(str) {
    return str.replace(/[&<>'"]/g, tag => ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        "'": '&#39;',
        '"': '&quot;'
    }[tag] || tag));
}

// 模态弹窗控制
const modal = document.getElementById('modal-edit');
const editType = document.getElementById('edit-type');
const editPath = document.getElementById('edit-path');
const editState = document.getElementById('edit-state');
const editJson = document.getElementById('edit-json');

function openEditModal(item) {
    editType.value = item.type;
    editPath.value = item.path;
    const st = parseState(item.status);
    editState.value = (st === 'completed' || st === 'in_progress' || st === 'abandoned') ? st : 'pending';
    editJson.value = item.status || `{"state":"${editState.value}"}`;
    modal.classList.remove('hidden');
}

document.getElementById('modal-close').onclick = () => modal.classList.add('hidden');
document.getElementById('btn-cancel').onclick = () => modal.classList.add('hidden');

editState.onchange = () => {
    try {
        let obj = JSON.parse(editJson.value);
        obj.state = editState.value;
        editJson.value = JSON.stringify(obj, null, 2);
    } catch {
        editJson.value = JSON.stringify({ state: editState.value }, null, 2);
    }
};

document.getElementById('form-update').onsubmit = async (e) => {
    e.preventDefault();
    try {
        const payload = {
            type: editType.value,
            path: editPath.value,
            status: editJson.value.trim()
        };
        await fetchJSON('/api/v1/append', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        modal.classList.add('hidden');
        await loadData();
    } catch (err) {
        alert('追加保存失败: ' + err.message);
    }
};

// 头部操作按钮
document.getElementById('btn-refresh').onclick = loadData;

document.getElementById('btn-check').onclick = async () => {
    try {
        const res = await fetchJSON('/api/v1/check', { method: 'POST' });
        if (res.valid) {
            alert(`✅ SQL 文件校验完全正常！共包含 ${res.total_statements} 条有效语句。`);
        } else {
            alert(`❌ 检测到 SQL 错误:\n` + res.errors.join('\n'));
        }
    } catch (err) {
        alert('校验请求失败: ' + err.message);
    }
};

document.getElementById('btn-scan').onclick = async () => {
    try {
        const report = await fetchJSON('/api/v1/scan');
        const alertBanner = document.getElementById('alert-banner');
        if (report.unregistered.length > 0 || report.dangling.length > 0) {
            alertBanner.innerHTML = `⚠️ 发现 <strong>${report.unregistered.length}</strong> 篇未在 SQL 登记的文档，<strong>${report.dangling.length}</strong> 条悬空记录。`;
            alertBanner.classList.remove('hidden');
        } else {
            alertBanner.classList.add('hidden');
            alert(`🎉 扫描对齐完毕！共扫描到 ${report.total_discovered} 篇文档，全部已准确在 SQL 中完成登记。`);
        }
    } catch (err) {
        alert('扫描失败: ' + err.message);
    }
};

// 初始化
loadData();
