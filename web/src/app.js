// Polls the Go backend and renders live state into the page sections. The
// backend serves canonical JSON; this page reflects the persisted pipeline
// state: topology, isolation matrix, reading timeline, samples, retests, and
// the terminal verdict.

const $ = (id) => document.getElementById(id);

function setStatus(text, ok) {
  const el = $('status');
  el.textContent = text;
  el.className = 'status ' + (ok ? 'ok' : 'err');
}

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(url + ' -> ' + res.status);
  return res.json();
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

function table(headers, rows) {
  const thead = '<tr>' + headers.map((h) => `<th>${esc(h)}</th>`).join('') + '</tr>';
  const tbody = rows.map((r) => '<tr>' + r.map((c) => `<td>${esc(c)}</td>`).join('') + '</tr>').join('');
  return '<table><thead>' + thead + '</thead><tbody>' + tbody + '</tbody></table>';
}

function renderJob(job) {
  if (!job) {
    $('topology-body').textContent = '暂无已锁定作业';
    return;
  }
  $('topology-body').innerHTML = table(
    ['作业', '阶段', '轮次', '逻辑时钟', '规则版本', '拓扑摘要'],
    [[job.id, job.stage, job.round, job.clock, job.rule_version, job.topology_digest]]
  );
}

function renderTopology(topo) {
  if (!topo) {
    $('valves-body').textContent = '暂无拓扑';
    return;
  }
  const valves = (topo.valves || []).map((v) => [v.id, v.section_id, v.closed ? '已关闭' : '未关闭']);
  $('valves-body').innerHTML = table(['阀门', '管段', '状态'], valves);
  const sampling = (topo.sampling || []).map((s) => [s.id, s.section_id, s.order]);
  $('window-body').innerHTML =
    '<p>采样点（锁定顺序）</p>' + table(['采样点', '管段', '顺序'], sampling);
}

function renderSamples(samples) {
  if (!samples || !samples.length) {
    $('samples-body').textContent = '暂无样本';
    return;
  }
  const rows = samples.map((s) => [s.id, s.point_id, s.round, s.label, s.collected_by, s.sealed_by]);
  $('samples-body').innerHTML = table(['样本', '采样点', '轮次', '标签', '采集人', '封存人'], rows);
}

function renderRetests(retests) {
  if (!retests || !retests.length) {
    $('retests-body').textContent = '暂无复验集合';
    return;
  }
  const rows = retests.map((r) => [r.id, r.round, (r.members || []).join(', ')]);
  $('retests-body').innerHTML = table(['复验', '轮次', '影响采样点'], rows);
}

function renderMeasurements(measurements) {
  if (!measurements || !measurements.length) {
    $('timeline-body').textContent = '暂无读数';
    return;
  }
  const rows = measurements.map((m) => [
    m.instrument_id,
    m.clock,
    (m.readings || []).map((q) => q.value).join(', '),
  ]);
  $('timeline-body').innerHTML = table(['仪器', '逻辑时钟', '读数'], rows);
}

async function refresh() {
  try {
    const health = await fetchJSON('/health');
    const list = await fetchJSON('/api/jobs');
    const jobs = list.jobs || [];
    setStatus('已连接后端 · 运行 ' + health.uptime_s + ' 秒 · 作业 ' + health.jobs, true);
    $('backend').textContent = health.component || '-';

    if (!jobs.length) {
      renderJob(null);
      ['valves-body', 'window-body', 'samples-body', 'retests-body', 'timeline-body', 'terminal-body'].forEach((id) => {
        $(id).textContent = '暂无数据';
      });
      return;
    }

    const job = jobs[0];
    renderJob(job);

    const [topo, samples, retests, measurements] = await Promise.all([
      fetchJSON('/api/jobs/' + job.id + '/topology'),
      fetchJSON('/api/jobs/' + job.id + '/samples'),
      fetchJSON('/api/jobs/' + job.id + '/retests'),
      fetchJSON('/api/jobs/' + job.id + '/measurements'),
    ]);
    renderTopology(topo);
    renderSamples(samples.samples || []);
    renderRetests(retests.retests || []);
    renderMeasurements(measurements.measurements || []);

    if (job.stage === 'terminal_verdict') {
      $('terminal-body').textContent = '作业 ' + job.id + ' 已进入终局裁定';
    } else {
      $('terminal-body').textContent = '当前阶段：' + job.stage + '（双人复核后生成唯一通水凭据）';
    }
  } catch (err) {
    setStatus('连接后端失败: ' + err.message, false);
  }
}

refresh();
setInterval(refresh, 3000);
