'use strict';

const $ = (id) => document.getElementById(id);

const ACTION_LABELS = {
  whitelisted: { text: '放行', cls: 'ok' },
  dropped: { text: '丢弃', cls: 'drop' },
  parse_failed: { text: '解析失败', cls: 'fail' },
  pushed: { text: '已推送', cls: 'ok' },
  push_failed: { text: '推送失败', cls: 'fail' },
};

let rules = [];
let toastTimer = null;

async function request(path, options = {}) {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  let payload;
  try {
    payload = await response.json();
  } catch {
    throw new Error(`HTTP ${response.status}`);
  }
  if (!payload.ok) {
    throw new Error(payload.error?.message || `HTTP ${response.status}`);
  }
  return payload.data;
}

function toast(message, kind = 'ok') {
  const node = $('toast');
  node.textContent = message;
  node.className = `toast show${kind === 'error' ? ' error' : ''}`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    node.className = 'toast';
  }, 3200);
}

function fmtTime(value) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() < 2000) return '—';
  return date.toLocaleString('zh-CN', { hour12: false });
}

function renderStatus(status) {
  $('conn-dot').className = 'dot ' + (status.connected ? 'ok' : status.running ? 'warn' : 'off');
  $('conn-text').textContent = status.connected ? '已连接' : status.running ? '连接中…' : '已停止';
  $('version').textContent = `${status.version} · ${status.mode}`;

  $('stat-conn').textContent = status.connected ? '在线' : status.running ? '连接中' : '停止';
  $('stat-conn-hint').textContent = status.last_error ? '存在错误，见下方' : 'IMAP IDLE 长连接';
  $('stat-uid').textContent = status.last_uid > 0 ? status.last_uid : '—';
  $('stat-rules').textContent = status.rules;
  $('stat-push').textContent = status.last_push_at ? (status.last_push_ok ? '成功' : '失败') : '—';
  $('stat-push-hint').textContent = status.last_push_at ? fmtTime(status.last_push_at) : '尚未推送';

  $('detail-account').textContent = status.username || '—';
  $('detail-mailbox').textContent = status.mailbox || '—';
  $('detail-last-mail').textContent = fmtTime(status.last_mail_at);
  $('detail-started').textContent = fmtTime(status.started_at);
  $('detail-config').textContent = status.config_path || '—';
  $('detail-data').textContent = status.data_dir || '—';

  const errorNode = $('detail-error');
  if (status.last_error) {
    errorNode.textContent = status.last_error;
    errorNode.classList.remove('hidden');
  } else {
    errorNode.classList.add('hidden');
  }
}

async function loadStatus() {
  const status = await request('/api/status');
  renderStatus(status);
}

async function loadConfig() {
  const config = await request('/api/config');
  fillConfig(config);
}

function fillConfig(config) {
  $('imap-host').value = config.imap.host || '';
  $('imap-port').value = config.imap.port || '';
  $('imap-username').value = config.imap.username || '';
  $('imap-password').value = '';
  $('imap-mailbox').value = config.imap.mailbox || '';
  $('imap-idle').value = config.imap.idle_timeout || '';
  $('imap-poll').value = config.imap.poll_interval || '';
  $('feishu-webhook').value = config.feishu.webhook_url || '';
  $('feishu-secret').value = '';
  $('rl-debounce').value = config.ratelimit.debounce_window || '';
  $('rl-cooldown').value = config.ratelimit.thread_cooldown || '';
  $('password-hint').classList.toggle('hidden', !config.imap.password_set);
  $('secret-hint').classList.toggle('hidden', !config.feishu.secret_set);

  rules = (config.whitelist || []).map((rule) => ({
    name: rule.name || '',
    emails: (rule.sender_emails || []).join(', '),
    domains: (rule.sender_domains || []).join(', '),
    keywords: (rule.subject_keywords || []).join(', '),
  }));
  renderRules();
}

function renderRules() {
  const list = $('rule-list');
  list.textContent = '';

  if (rules.length === 0) {
    const empty = document.createElement('p');
    empty.className = 'hint';
    empty.textContent = '暂无规则；所有邮件都会被静默丢弃。';
    list.appendChild(empty);
    return;
  }

  rules.forEach((rule, index) => {
    const card = document.createElement('div');
    card.className = 'rule-card';

    const head = document.createElement('div');
    head.className = 'rule-head';

    const nameInput = document.createElement('input');
    nameInput.className = 'rule-name';
    nameInput.placeholder = '规则名（如 github）';
    nameInput.value = rule.name;
    nameInput.addEventListener('input', () => {
      rules[index].name = nameInput.value;
    });

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'button danger-ghost';
    remove.textContent = '删除';
    remove.addEventListener('click', () => {
      rules.splice(index, 1);
      renderRules();
    });

    head.append(nameInput, remove);
    card.appendChild(head);
    card.appendChild(makeRuleField('发件人邮箱（逗号分隔）', rule.emails, (value) => {
      rules[index].emails = value;
    }));
    card.appendChild(makeRuleField('发件人域名（逗号分隔）', rule.domains, (value) => {
      rules[index].domains = value;
    }));
    card.appendChild(makeRuleField('主题关键词（逗号分隔）', rule.keywords, (value) => {
      rules[index].keywords = value;
    }));

    list.appendChild(card);
  });
}

function makeRuleField(labelText, value, onChange) {
  const label = document.createElement('label');
  label.textContent = labelText;
  const input = document.createElement('input');
  input.value = value;
  input.addEventListener('input', () => onChange(input.value));
  label.appendChild(input);
  return label;
}

function splitList(value) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
}

function collectConfig() {
  return {
    imap: {
      host: $('imap-host').value.trim(),
      port: Number($('imap-port').value) || 0,
      username: $('imap-username').value.trim(),
      password: $('imap-password').value,
      mailbox: $('imap-mailbox').value.trim(),
      idle_timeout: $('imap-idle').value.trim(),
      poll_interval: $('imap-poll').value.trim(),
    },
    feishu: {
      webhook_url: $('feishu-webhook').value.trim(),
      secret: $('feishu-secret').value,
    },
    ratelimit: {
      debounce_window: $('rl-debounce').value.trim(),
      thread_cooldown: $('rl-cooldown').value.trim(),
    },
    whitelist: rules
      .map((rule) => ({
        name: rule.name.trim(),
        sender_emails: splitList(rule.emails),
        sender_domains: splitList(rule.domains),
        subject_keywords: splitList(rule.keywords),
      }))
      .filter((rule) =>
        rule.name || rule.sender_emails.length || rule.sender_domains.length || rule.subject_keywords.length
      ),
  };
}

async function saveConfig(event) {
  event.preventDefault();
  const hint = $('save-hint');
  hint.textContent = '保存中…';
  try {
    const data = await request('/api/config', {
      method: 'PUT',
      body: JSON.stringify(collectConfig()),
    });
    fillConfig(data.config);
    renderStatus(data.status);
    hint.textContent = '';
    toast('配置已保存，监听已重启');
  } catch (error) {
    hint.textContent = '';
    toast(error.message, 'error');
  }
}

function cell(text, title) {
  const td = document.createElement('td');
  td.textContent = text;
  if (title) td.title = title;
  return td;
}

async function loadRecords() {
  const events = await request('/api/records?limit=200');
  const body = $('records-body');
  body.textContent = '';

  if (!events || events.length === 0) {
    const row = document.createElement('tr');
    const empty = document.createElement('td');
    empty.colSpan = 8;
    empty.className = 'empty';
    empty.textContent = '暂无记录';
    row.appendChild(empty);
    body.appendChild(row);
    return;
  }

  const fragment = document.createDocumentFragment();
  for (const event of events) {
    const row = document.createElement('tr');
    const action = ACTION_LABELS[event.action] || { text: event.action || '—', cls: '' };

    row.appendChild(cell(fmtTime(event.time)));
    row.appendChild(cell(event.type === 'push' ? '推送' : '邮件'));
    row.appendChild(cell(event.uid ? String(event.uid) : ''));
    row.appendChild(cell(event.from || ''));
    row.appendChild(cell(event.subject || '', event.subject));

    const actionCell = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = `badge ${action.cls}`;
    badge.textContent = action.text;
    actionCell.appendChild(badge);
    row.appendChild(actionCell);

    row.appendChild(cell(event.rule || event.reason || event.error || ''));
    row.appendChild(cell(event.duration_ms ? `${event.duration_ms}ms` : ''));
    fragment.appendChild(row);
  }
  body.appendChild(fragment);
}

async function loadLogs() {
  const data = await request('/api/logs?tail=200');
  $('logs').textContent = data.lines && data.lines.length ? data.lines.join('\n') : '暂无日志';
}

function switchTab(name) {
  document.querySelectorAll('.tab-button').forEach((button) => {
    button.classList.toggle('active', button.dataset.tab === name);
  });
  document.querySelectorAll('.tab-panel').forEach((panel) => {
    panel.classList.toggle('active', panel.id === `tab-${name}`);
  });
  if (name === 'config') loadConfig().catch((error) => toast(error.message, 'error'));
  if (name === 'records') {
    loadRecords().catch((error) => toast(error.message, 'error'));
    loadLogs().catch((error) => toast(error.message, 'error'));
  }
}

async function init() {
  try {
    await loadStatus();
  } catch {
    $('conn-text').textContent = '无法连接服务';
  }
  try {
    await loadConfig();
  } catch {
    // 配置加载失败时保留空表单，具体错误由操作时提示。
  }
  loadRecords().catch(() => {});
  loadLogs().catch(() => {});

  document.querySelectorAll('.tab-button').forEach((button) => {
    button.addEventListener('click', () => switchTab(button.dataset.tab));
  });
  $('config-form').addEventListener('submit', saveConfig);
  $('btn-add-rule').addEventListener('click', () => {
    rules.push({ name: '', emails: '', domains: '', keywords: '' });
    renderRules();
  });
  $('btn-refresh-records').addEventListener('click', () => {
    loadRecords().catch((error) => toast(error.message, 'error'));
  });
  $('btn-refresh-logs').addEventListener('click', () => {
    loadLogs().catch((error) => toast(error.message, 'error'));
  });
  $('btn-reconnect').addEventListener('click', async () => {
    try {
      renderStatus(await request('/api/reconnect', { method: 'POST' }));
      toast('已触发重连');
    } catch (error) {
      toast(error.message, 'error');
    }
  });
  $('btn-test').addEventListener('click', async () => {
    try {
      await request('/api/test-webhook', { method: 'POST' });
      toast('测试卡片已发送');
    } catch (error) {
      toast(error.message, 'error');
    }
  });

  setInterval(() => {
    loadStatus().catch(() => {});
    if (document.querySelector('#tab-records.active')) {
      loadRecords().catch(() => {});
    }
  }, 5000);
}

init();
