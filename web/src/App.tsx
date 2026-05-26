import { FormEvent, useEffect, useState } from 'react';

type Job = {
  id: number;
  delivery_id: string;
  event_name: string;
  action: string;
  repo_full_name: string;
  owner: string;
  repo: string;
  pr_number: number;
  head_sha: string;
  base_sha: string;
  sender: string;
  status: string;
  attempt_count: number;
  error_message?: string;
  summary?: string;
  gitea_comment_id?: string;
  input_tokens?: number;
  output_tokens?: number;
  estimated_cost?: number;
  created_at: string;
};

type Finding = {
  id: number;
  job_id: number;
  path: string;
  line?: number;
  severity: string;
  category: string;
  title: string;
  body: string;
  confidence?: number;
  is_inline: boolean;
  is_posted: boolean;
  gitea_comment_url?: string;
  post_error?: string;
};

type JobEvent = {
  id: number;
  job_id: number;
  type: string;
  message?: string;
  created_at: string;
};

type WebhookDelivery = {
  id: number;
  delivery_id: string;
  event_name: string;
  action: string;
  repo_full_name: string;
  pr_number?: number;
  head_sha: string;
  sender: string;
  signature_valid: boolean;
  status: string;
  error_message?: string;
  job_id?: number;
  received_at: string;
};

type RuntimeSettings = {
  gitea_base_url: string;
  gitea_token?: string;
  gitea_webhook_secret?: string;
  bot_name: string;
  openai_api_key?: string;
  openai_base_url: string;
  review_model: string;
  review_language: string;
  review_profile: string;
  review_focus_areas: string[];
  review_output_style: string;
  review_extra_instructions: string;
  review_input_token_price_per_million: number;
  review_output_token_price_per_million: number;
  review_max_diff_bytes: number;
  review_exclude_paths: string[];
  review_fail_on_high: boolean;
  review_post_inline_comments: boolean;
  review_max_findings: number;
  review_max_attempts: number;
  review_stale_timeout: string;
  worker_poll_interval: string;
  has_gitea_token?: boolean;
  has_gitea_webhook_secret?: boolean;
  has_openai_api_key?: boolean;
};

type JobsResponse = {
  jobs: Job[];
};

type FindingsResponse = {
  findings: Finding[];
};

type DeliveriesResponse = {
  deliveries: WebhookDelivery[];
};

type EventsResponse = {
  events: JobEvent[];
};

type Mode = 'loading' | 'setup' | 'login' | 'app';
type AppTab = 'jobs' | 'deliveries' | 'settings';

const defaultSettings: RuntimeSettings = {
  gitea_base_url: '',
  gitea_token: '',
  gitea_webhook_secret: '',
  bot_name: 'gpt-review-bot',
  openai_api_key: '',
  openai_base_url: 'https://api.openai.com/v1',
  review_model: 'gpt-4.1',
  review_language: '中文',
  review_profile: 'balanced',
  review_focus_areas: ['correctness', 'security', 'data_loss', 'concurrency', 'test_gap'],
  review_output_style: 'detailed',
  review_extra_instructions: '',
  review_input_token_price_per_million: 0,
  review_output_token_price_per_million: 0,
  review_max_diff_bytes: 120000,
  review_exclude_paths: ['vendor/**', 'node_modules/**', 'dist/**', 'build/**', '*.lock', '*.min.js'],
  review_fail_on_high: true,
  review_post_inline_comments: false,
  review_max_findings: 20,
  review_max_attempts: 3,
  review_stale_timeout: '10m0s',
  worker_poll_interval: '5s',
};

export function App() {
  const [mode, setMode] = useState<Mode>('loading');
  const [tab, setTab] = useState<AppTab>('jobs');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function bootstrap() {
      try {
        const setupResponse = await fetch('/api/setup/status');
        if (!setupResponse.ok) {
          throw new Error(`初始化状态请求失败：${setupResponse.status}`);
        }
        const setupData = (await setupResponse.json()) as { initialized: boolean };
        if (!setupData.initialized) {
          setMode('setup');
          return;
        }
        const meResponse = await fetch('/api/me');
        setMode(meResponse.ok ? 'app' : 'login');
      } catch (err) {
        setError(err instanceof Error ? err.message : '系统初始化失败');
        setMode('login');
      }
    }

    void bootstrap();
  }, []);

  if (mode === 'loading') {
    return <main className="page"><p className="empty">加载中...</p></main>;
  }

  if (mode === 'setup') {
    return <SetupPage onDone={() => setMode('app')} />;
  }

  if (mode === 'login') {
    return <LoginPage onDone={() => setMode('app')} />;
  }

  return (
    <main className="page">
      <header className="header">
        <div>
          <p className="eyebrow">Gitea PR Code Review Bot</p>
          <h1>{tab === 'jobs' ? '审查任务' : tab === 'deliveries' ? 'Webhook 记录' : '系统配置'}</h1>
        </div>
        <div className="headerActions">
          <button className={tab === 'jobs' ? 'secondary activeTab' : 'secondary'} onClick={() => setTab('jobs')}>任务</button>
          <button className={tab === 'deliveries' ? 'secondary activeTab' : 'secondary'} onClick={() => setTab('deliveries')}>Webhook</button>
          <button className={tab === 'settings' ? 'secondary activeTab' : 'secondary'} onClick={() => setTab('settings')}>配置</button>
          <button onClick={() => logout().finally(() => setMode('login'))}>退出</button>
        </div>
      </header>

      {error ? <div className="alert">{error}</div> : null}
      {tab === 'jobs' ? <JobsPage /> : tab === 'deliveries' ? <DeliveriesPage /> : <SettingsPage />}
    </main>
  );
}

function SetupPage({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [settings, setSettings] = useState(defaultSettings);
  const [error, setError] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setIsSaving(true);
    setError(null);
    try {
      const response = await fetch('/api/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password, settings: normalizeSettings(settings) }),
      });
      if (!response.ok) {
        throw new Error(`安装失败：${response.status}`);
      }
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : '安装失败');
    } finally {
      setIsSaving(false);
    }
  }

  return (
    <main className="page narrow">
      <header className="header">
        <div>
          <p className="eyebrow">首次安装</p>
          <h1>初始化 Code Review Bot</h1>
        </div>
      </header>
      {error ? <div className="alert">{error}</div> : null}
      <form className="panel formPanel" onSubmit={(event) => void submit(event)}>
        <h2>管理员账号</h2>
        <label>用户名<input value={username} onChange={(event) => setUsername(event.target.value)} required /></label>
        <label>密码<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} minLength={8} required /></label>
        <h2>运行配置</h2>
        <SettingsFields settings={settings} onChange={setSettings} sensitivePlaceholder="" />
        <button disabled={isSaving}>{isSaving ? '安装中...' : '完成安装'}</button>
      </form>
    </main>
  );
}

function LoginPage({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setIsLoading(true);
    setError(null);
    try {
      const response = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });
      if (!response.ok) {
        throw new Error(`登录失败：${response.status}`);
      }
      onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败');
    } finally {
      setIsLoading(false);
    }
  }

  return (
    <main className="page narrow">
      <header className="header">
        <div>
          <p className="eyebrow">管理员登录</p>
          <h1>登录</h1>
        </div>
      </header>
      {error ? <div className="alert">{error}</div> : null}
      <form className="panel formPanel" onSubmit={(event) => void submit(event)}>
        <label>用户名<input value={username} onChange={(event) => setUsername(event.target.value)} required /></label>
        <label>密码<input type="password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
        <button disabled={isLoading}>{isLoading ? '登录中...' : '登录'}</button>
      </form>
    </main>
  );
}

function JobsPage() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [selectedJob, setSelectedJob] = useState<Job | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function loadJobs() {
    setIsLoading(true);
    setError(null);

    try {
      const response = await fetch('/api/jobs');
      if (!response.ok) {
        throw new Error(`请求失败：${response.status}`);
      }
      const data = (await response.json()) as JobsResponse;
      const nextJobs = data.jobs ?? [];
      setJobs(nextJobs);
      setSelectedJob((current) => {
        if (!current) {
          return nextJobs[0] ?? null;
        }
        return nextJobs.find((job) => job.id === current.id) ?? nextJobs[0] ?? null;
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载任务失败');
    } finally {
      setIsLoading(false);
    }
  }

  async function retryJob(job: Job) {
    setError(null);
    const response = await fetch(`/api/jobs/${job.id}/retry`, { method: 'POST' });
    if (!response.ok) {
      throw new Error(`重试失败：${response.status}`);
    }
    await loadJobs();
  }

  useEffect(() => {
    void loadJobs();
  }, []);

  return (
    <>
      <div className="pageActions"><button onClick={() => void loadJobs()} disabled={isLoading}>{isLoading ? '刷新中...' : '刷新'}</button></div>
      {error ? <div className="alert">{error}</div> : null}
      <section className="layout">
        <div className="panel">
          <div className="panelHeader"><h2>任务列表</h2><span>{jobs.length} 条</span></div>
          <div className="jobList">
            {jobs.length === 0 && !isLoading ? <p className="empty">暂无 review job</p> : null}
            {jobs.map((job) => (
              <button key={job.id} className={job.id === selectedJob?.id ? 'job active' : 'job'} onClick={() => setSelectedJob(job)}>
                <div className="jobTopline"><strong>#{job.pr_number}</strong><StatusBadge status={job.status} /></div>
                <span>{job.repo_full_name}</span>
                <small>{shortSha(job.head_sha)} · 尝试 {job.attempt_count} 次</small>
              </button>
            ))}
          </div>
        </div>

        <div className="panel detail">
          <div className="panelHeader"><h2>任务详情</h2></div>
          {selectedJob ? (
            <JobDetail job={selectedJob} onRetry={(job) => retryJob(job).catch((err) => setError(err instanceof Error ? err.message : '重试失败'))} />
          ) : <p className="empty">请选择一个任务</p>}
        </div>
      </section>
    </>
  );
}

function DeliveriesPage() {
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function loadDeliveries() {
    setIsLoading(true);
    setError(null);
    try {
      const response = await fetch('/api/deliveries');
      if (!response.ok) {
        throw new Error(`请求失败：${response.status}`);
      }
      const data = (await response.json()) as DeliveriesResponse;
      setDeliveries(data.deliveries ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载 webhook delivery 失败');
    } finally {
      setIsLoading(false);
    }
  }

  useEffect(() => {
    void loadDeliveries();
  }, []);

  return (
    <>
      <div className="pageActions"><button onClick={() => void loadDeliveries()} disabled={isLoading}>{isLoading ? '刷新中...' : '刷新'}</button></div>
      {error ? <div className="alert">{error}</div> : null}
      <section className="panel">
        <div className="panelHeader"><h2>Webhook 记录</h2><span>{deliveries.length} 条</span></div>
        {deliveries.length === 0 && !isLoading ? <p className="empty">暂无 Webhook 记录</p> : null}
        <div className="findingList">
          {deliveries.map((delivery) => (
            <article key={delivery.id} className="finding">
              <div className="findingTopline"><strong>{delivery.repo_full_name || '-'}</strong><StatusBadge status={delivery.status} /></div>
              <small>
                {delivery.event_name}{delivery.action ? ` / ${delivery.action}` : ''}
                {delivery.pr_number ? ` · PR #${delivery.pr_number}` : ''}
                {delivery.job_id ? ` · job ${delivery.job_id}` : ''}
                {delivery.sender ? ` · ${delivery.sender}` : ''}
                {` · ${new Date(delivery.received_at).toLocaleString()}`}
              </small>
              <p><code>{delivery.delivery_id}</code>{delivery.head_sha ? ` · ${shortSha(delivery.head_sha)}` : ''}</p>
              {delivery.error_message ? <p className="errorText">{delivery.error_message}</p> : null}
            </article>
          ))}
        </div>
      </section>
    </>
  );
}

function SettingsPage() {
  const [settings, setSettings] = useState<RuntimeSettings>(defaultSettings);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    async function loadSettings() {
      setIsLoading(true);
      try {
        const response = await fetch('/api/settings');
        if (!response.ok) {
          throw new Error(`加载配置失败：${response.status}`);
        }
        const data = (await response.json()) as { settings: RuntimeSettings };
        setSettings({ ...defaultSettings, ...data.settings, gitea_token: '', gitea_webhook_secret: '', openai_api_key: '' });
      } catch (err) {
        setError(err instanceof Error ? err.message : '加载配置失败');
      } finally {
        setIsLoading(false);
      }
    }
    void loadSettings();
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setMessage(null);
    const response = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(normalizeSettings(settings)),
    });
    if (!response.ok) {
      setError(`保存配置失败：${response.status}`);
      return;
    }
    setMessage('配置已保存');
  }

  async function testSettings(kind: 'gitea' | 'openai') {
    const label = kind === 'gitea' ? 'Gitea' : 'OpenAI';
    setError(null);
    setMessage(null);
    try {
      const response = await fetch(`/api/settings/test-${kind}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(normalizeSettings(settings)),
      });
      if (!response.ok) {
        setError(`${label} 连通性测试失败：${response.status}`);
        return;
      }
      setMessage(`${label} 连通性测试通过`);
    } catch (error) {
      setError(`${label} 连通性测试失败：${error instanceof Error ? error.message : '网络错误'}`);
    }
  }

  if (isLoading) {
    return <p className="empty">加载配置中...</p>;
  }

  return (
    <form className="panel formPanel" onSubmit={(event) => void submit(event)}>
      {error ? <div className="alert">{error}</div> : null}
      {message ? <div className="success">{message}</div> : null}
      <SettingsFields settings={settings} onChange={setSettings} sensitivePlaceholder="留空则保留当前值" />
      <div className="detailActions">
        <button type="button" className="secondary" onClick={() => void testSettings('gitea')}>测试 Gitea</button>
        <button type="button" className="secondary" onClick={() => void testSettings('openai')}>测试 OpenAI</button>
        <button>保存配置</button>
      </div>
    </form>
  );
}

function SettingsFields({ settings, onChange, sensitivePlaceholder }: { settings: RuntimeSettings; onChange: (settings: RuntimeSettings) => void; sensitivePlaceholder: string }) {
  const excludePathsText = settings.review_exclude_paths.join(',');
  const focusAreasText = settings.review_focus_areas.join(',');

  function patch(next: Partial<RuntimeSettings>) {
    onChange({ ...settings, ...next });
  }

  return (
    <div className="settingsGrid">
      <label>Gitea 地址<input value={settings.gitea_base_url} onChange={(event) => patch({ gitea_base_url: event.target.value })} placeholder="https://gitea.example.com" /></label>
      <label>Gitea Token<input type="password" value={settings.gitea_token ?? ''} onChange={(event) => patch({ gitea_token: event.target.value })} placeholder={sensitivePlaceholder} /></label>
      <label>Webhook 密钥<input type="password" value={settings.gitea_webhook_secret ?? ''} onChange={(event) => patch({ gitea_webhook_secret: event.target.value })} placeholder={sensitivePlaceholder} /></label>
      <label>机器人名称<input value={settings.bot_name} onChange={(event) => patch({ bot_name: event.target.value })} /></label>
      <label>OpenAI API Key<input type="password" value={settings.openai_api_key ?? ''} onChange={(event) => patch({ openai_api_key: event.target.value })} placeholder={sensitivePlaceholder} /></label>
      <label>OpenAI 地址<input value={settings.openai_base_url} onChange={(event) => patch({ openai_base_url: event.target.value })} /></label>
      <label>审查模型<input value={settings.review_model} onChange={(event) => patch({ review_model: event.target.value })} /></label>
      <label>输出语言<input value={settings.review_language} onChange={(event) => patch({ review_language: event.target.value })} placeholder="中文" /></label>
      <label>审查强度<select value={settings.review_profile} onChange={(event) => patch({ review_profile: event.target.value })}><option value="strict">严格</option><option value="balanced">平衡</option><option value="lenient">宽松</option></select></label>
      <label>输出风格<select value={settings.review_output_style} onChange={(event) => patch({ review_output_style: event.target.value })}><option value="concise">简洁</option><option value="detailed">详细</option></select></label>
      <label className="wideField">关注领域<input value={focusAreasText} onChange={(event) => patch({ review_focus_areas: splitList(event.target.value) })} placeholder="correctness,security,data_loss,concurrency,test_gap" /></label>
      <label className="wideField">额外审查规则<textarea value={settings.review_extra_instructions} onChange={(event) => patch({ review_extra_instructions: event.target.value })} placeholder="例如：全部使用中文输出，重点关注 SQL 注入、XSS、并发和数据丢失问题。" rows={5} /></label>
      <label>输入 Token 单价/百万<input type="number" step="0.000001" value={settings.review_input_token_price_per_million} onChange={(event) => patch({ review_input_token_price_per_million: Number(event.target.value) })} /></label>
      <label>输出 Token 单价/百万<input type="number" step="0.000001" value={settings.review_output_token_price_per_million} onChange={(event) => patch({ review_output_token_price_per_million: Number(event.target.value) })} /></label>
      <label>最大 Diff 字节数<input type="number" value={settings.review_max_diff_bytes} onChange={(event) => patch({ review_max_diff_bytes: Number(event.target.value) })} /></label>
      <label>排除路径<input value={excludePathsText} onChange={(event) => patch({ review_exclude_paths: splitList(event.target.value) })} /></label>
      <label>最大 Findings 数<input type="number" value={settings.review_max_findings} onChange={(event) => patch({ review_max_findings: Number(event.target.value) })} /></label>
      <label>最大尝试次数<input type="number" value={settings.review_max_attempts} onChange={(event) => patch({ review_max_attempts: Number(event.target.value) })} /></label>
      <label>Stale 超时时间<input value={settings.review_stale_timeout} onChange={(event) => patch({ review_stale_timeout: event.target.value })} /></label>
      <label>Worker 轮询间隔<input value={settings.worker_poll_interval} onChange={(event) => patch({ worker_poll_interval: event.target.value })} /></label>
      <label className="checkbox"><input type="checkbox" checked={settings.review_fail_on_high} onChange={(event) => patch({ review_fail_on_high: event.target.checked })} />High risk 设置 failure</label>
      <label className="checkbox"><input type="checkbox" checked={settings.review_post_inline_comments} onChange={(event) => patch({ review_post_inline_comments: event.target.checked })} />发布 inline comments</label>
    </div>
  );
}

function JobDetail({ job, onRetry }: { job: Job; onRetry: (job: Job) => void }) {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [isLoadingFindings, setIsLoadingFindings] = useState(false);
  const [findingsError, setFindingsError] = useState<string | null>(null);
  const [eventsError, setEventsError] = useState<string | null>(null);

  useEffect(() => {
    let isActive = true;

    async function loadFindings() {
      setIsLoadingFindings(true);
      setFindingsError(null);
      setEventsError(null);
      setEvents([]);
      try {
        const response = await fetch(`/api/jobs/${job.id}/findings`);
        if (!response.ok) {
          throw new Error(`请求失败：${response.status}`);
        }
        const data = (await response.json()) as FindingsResponse;
        if (isActive) {
          setFindings(data.findings ?? []);
        }
      } catch (err) {
        if (isActive) {
          setFindings([]);
          setFindingsError(err instanceof Error ? err.message : '加载 findings 失败');
        }
      } finally {
        if (isActive) {
          setIsLoadingFindings(false);
        }
      }

      try {
        const eventsResponse = await fetch(`/api/jobs/${job.id}/events`);
        if (!eventsResponse.ok) {
          throw new Error(`请求失败：${eventsResponse.status}`);
        }
        const eventsData = (await eventsResponse.json()) as EventsResponse;
        if (isActive) {
          setEvents(eventsData.events ?? []);
        }
      } catch (err) {
        if (isActive) {
          setEvents([]);
          setEventsError(err instanceof Error ? err.message : '加载 timeline 失败');
        }
      }
    }

    void loadFindings();
    return () => {
      isActive = false;
    };
  }, [job.id]);

  return (
    <div>
      <dl className="detailGrid">
        <dt>ID</dt><dd>{job.id}</dd>
        <dt>状态</dt><dd><StatusBadge status={job.status} /></dd>
        <dt>尝试次数</dt><dd>{job.attempt_count}</dd>
        <dt>仓库</dt><dd>{job.repo_full_name}</dd>
        <dt>PR</dt><dd>#{job.pr_number}</dd>
        <dt>事件</dt><dd>{job.event_name} / {job.action || '-'}</dd>
        <dt>提交</dt><dd><code>{job.head_sha}</code></dd>
        <dt>触发人</dt><dd>{job.sender || '-'}</dd>
        <dt>创建时间</dt><dd>{new Date(job.created_at).toLocaleString()}</dd>
        <dt>Token 用量</dt><dd>{formatUsage(job)}</dd>
        <dt>估算成本</dt><dd>{formatCost(job.estimated_cost)}</dd>
        <dt>审查摘要</dt><dd>{job.summary || '-'}</dd>
        <dt>错误</dt><dd className={job.error_message ? 'errorText' : undefined}>{job.error_message || '-'}</dd>
      </dl>

      {isRetryable(job) ? <div className="detailActions"><button onClick={() => onRetry(job)}>重新排队 review</button></div> : null}

      <section className="findingsSection">
        <div className="panelHeader compact"><h2>执行时间线</h2><span>{events.length} 条</span></div>
        {eventsError ? <div className="alert">{eventsError}</div> : null}
        {events.length === 0 ? <p className="empty">暂无执行事件</p> : null}
        <div className="eventList">
          {events.map((event) => (
            <article key={event.id} className="eventItem">
              <strong>{eventLabel(event.type)}</strong>
              <span>{new Date(event.created_at).toLocaleString()}</span>
              {event.message ? <p>{event.message}</p> : null}
            </article>
          ))}
        </div>
      </section>

      <section className="findingsSection">
        <div className="panelHeader compact"><h2>审查发现</h2><span>{isLoadingFindings ? '加载中...' : `${findings.length} 条`}</span></div>
        {findingsError ? <div className="alert">{findingsError}</div> : null}
        {findings.length === 0 && !isLoadingFindings ? <p className="empty">暂无审查发现</p> : null}
        <div className="findingList">
          {findings.map((finding) => (
            <article key={finding.id} className="finding">
              <div className="findingTopline"><strong>{finding.title}</strong><span className={`badge ${finding.severity}`}>{finding.severity}</span></div>
              <p>{finding.body}</p>
              <small>
                {finding.category} · {finding.path}{finding.line ? `:${finding.line}` : ''}
                {typeof finding.confidence === 'number' ? ` · 置信度 ${finding.confidence.toFixed(2)}` : ''}
                {finding.is_inline ? ` · inline ${finding.is_posted ? '已发布' : '待发布'}` : ''}
              </small>
              {finding.gitea_comment_url ? <a href={finding.gitea_comment_url} target="_blank" rel="noreferrer">查看 inline comment</a> : null}
              {finding.post_error ? <p className="errorText">Inline 发布失败：{finding.post_error}</p> : null}
            </article>
          ))}
        </div>
      </section>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  return <span className={`badge ${status}`}>{statusLabel(status)}</span>;
}

function eventLabel(type: string) {
  const labels: Record<string, string> = {
    queued: '已排队',
    claimed: '已领取',
    fetch_files: '拉取文件',
    fetch_diff: '拉取 Diff',
    model_review: '模型审查',
    model_reviewed: '模型审查完成',
    usage_saved: '记录用量',
    findings_saved: '保存 Findings',
    summary_comment: '写入摘要评论',
    completed: '任务完成',
    failed: '任务失败',
  };
  return labels[type] ?? type;
}

function statusLabel(status: string) {
  const labels: Record<string, string> = {
    queued: '排队中',
    running: '运行中',
    succeeded: '已通过',
    failed: '未通过',
    errored: '执行错误',
    duplicate: '重复',
    ignored: '已忽略',
    ignored_bot_event: '忽略机器人事件',
    invalid_signature: '签名无效',
    invalid_payload: 'Payload 无效',
    error: '错误',
    received: '已接收',
    low: '低',
    medium: '中',
    high: '高',
  };
  return labels[status] ?? status;
}

function isRetryable(job: Job) {
  return job.status === 'errored';
}

function shortSha(sha: string) {
  return sha ? sha.slice(0, 8) : '-';
}

function formatUsage(job: Job) {
  const input = job.input_tokens ?? 0;
  const output = job.output_tokens ?? 0;
  if (input === 0 && output === 0) {
    return '-';
  }
  return `输入 ${input} / 输出 ${output}`;
}

function formatCost(value?: number) {
  if (!value || value <= 0) {
    return '-';
  }
  return `$${value.toFixed(6)}`;
}

function splitList(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean);
}

function normalizeSettings(settings: RuntimeSettings) {
  return {
    ...settings,
    review_max_diff_bytes: Number(settings.review_max_diff_bytes) || defaultSettings.review_max_diff_bytes,
    review_input_token_price_per_million: Number(settings.review_input_token_price_per_million) || 0,
    review_output_token_price_per_million: Number(settings.review_output_token_price_per_million) || 0,
    review_max_findings: Number(settings.review_max_findings) || defaultSettings.review_max_findings,
    review_max_attempts: Number(settings.review_max_attempts) || defaultSettings.review_max_attempts,
    review_exclude_paths: settings.review_exclude_paths,
    review_focus_areas: settings.review_focus_areas,
  };
}

async function logout() {
  await fetch('/api/logout', { method: 'POST' });
}
