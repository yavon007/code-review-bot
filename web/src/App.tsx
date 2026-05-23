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

type RuntimeSettings = {
  gitea_base_url: string;
  gitea_token?: string;
  gitea_webhook_secret?: string;
  bot_name: string;
  openai_api_key?: string;
  openai_base_url: string;
  review_model: string;
  review_max_diff_bytes: number;
  review_exclude_paths: string[];
  review_fail_on_high: boolean;
  review_post_inline_comments: boolean;
  review_max_findings: number;
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

type Mode = 'loading' | 'setup' | 'login' | 'app';
type AppTab = 'jobs' | 'settings';

const defaultSettings: RuntimeSettings = {
  gitea_base_url: '',
  gitea_token: '',
  gitea_webhook_secret: '',
  bot_name: 'gpt-review-bot',
  openai_api_key: '',
  openai_base_url: 'https://api.openai.com/v1',
  review_model: 'gpt-4.1',
  review_max_diff_bytes: 120000,
  review_exclude_paths: ['vendor/**', 'node_modules/**', 'dist/**', 'build/**', '*.lock', '*.min.js'],
  review_fail_on_high: true,
  review_post_inline_comments: false,
  review_max_findings: 20,
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
          <h1>{tab === 'jobs' ? 'Review Jobs' : '系统配置'}</h1>
        </div>
        <div className="headerActions">
          <button className={tab === 'jobs' ? 'secondary activeTab' : 'secondary'} onClick={() => setTab('jobs')}>任务</button>
          <button className={tab === 'settings' ? 'secondary activeTab' : 'secondary'} onClick={() => setTab('settings')}>配置</button>
          <button onClick={() => logout().finally(() => setMode('login'))}>退出</button>
        </div>
      </header>

      {error ? <div className="alert">{error}</div> : null}
      {tab === 'jobs' ? <JobsPage /> : <SettingsPage />}
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
          <p className="eyebrow">Admin Login</p>
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
                <small>{shortSha(job.head_sha)} · attempts {job.attempt_count}</small>
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

  if (isLoading) {
    return <p className="empty">加载配置中...</p>;
  }

  return (
    <form className="panel formPanel" onSubmit={(event) => void submit(event)}>
      {error ? <div className="alert">{error}</div> : null}
      {message ? <div className="success">{message}</div> : null}
      <SettingsFields settings={settings} onChange={setSettings} sensitivePlaceholder="留空则保留当前值" />
      <button>保存配置</button>
    </form>
  );
}

function SettingsFields({ settings, onChange, sensitivePlaceholder }: { settings: RuntimeSettings; onChange: (settings: RuntimeSettings) => void; sensitivePlaceholder: string }) {
  const excludePathsText = settings.review_exclude_paths.join(',');

  function patch(next: Partial<RuntimeSettings>) {
    onChange({ ...settings, ...next });
  }

  return (
    <div className="settingsGrid">
      <label>Gitea Base URL<input value={settings.gitea_base_url} onChange={(event) => patch({ gitea_base_url: event.target.value })} placeholder="https://gitea.example.com" /></label>
      <label>Gitea Token<input type="password" value={settings.gitea_token ?? ''} onChange={(event) => patch({ gitea_token: event.target.value })} placeholder={sensitivePlaceholder} /></label>
      <label>Webhook Secret<input type="password" value={settings.gitea_webhook_secret ?? ''} onChange={(event) => patch({ gitea_webhook_secret: event.target.value })} placeholder={sensitivePlaceholder} /></label>
      <label>Bot Name<input value={settings.bot_name} onChange={(event) => patch({ bot_name: event.target.value })} /></label>
      <label>OpenAI API Key<input type="password" value={settings.openai_api_key ?? ''} onChange={(event) => patch({ openai_api_key: event.target.value })} placeholder={sensitivePlaceholder} /></label>
      <label>OpenAI Base URL<input value={settings.openai_base_url} onChange={(event) => patch({ openai_base_url: event.target.value })} /></label>
      <label>Review Model<input value={settings.review_model} onChange={(event) => patch({ review_model: event.target.value })} /></label>
      <label>Max Diff Bytes<input type="number" value={settings.review_max_diff_bytes} onChange={(event) => patch({ review_max_diff_bytes: Number(event.target.value) })} /></label>
      <label>Exclude Paths<input value={excludePathsText} onChange={(event) => patch({ review_exclude_paths: splitList(event.target.value) })} /></label>
      <label>Max Findings<input type="number" value={settings.review_max_findings} onChange={(event) => patch({ review_max_findings: Number(event.target.value) })} /></label>
      <label>Worker Poll Interval<input value={settings.worker_poll_interval} onChange={(event) => patch({ worker_poll_interval: event.target.value })} /></label>
      <label className="checkbox"><input type="checkbox" checked={settings.review_fail_on_high} onChange={(event) => patch({ review_fail_on_high: event.target.checked })} />High risk 设置 failure</label>
      <label className="checkbox"><input type="checkbox" checked={settings.review_post_inline_comments} onChange={(event) => patch({ review_post_inline_comments: event.target.checked })} />发布 inline comments</label>
    </div>
  );
}

function JobDetail({ job, onRetry }: { job: Job; onRetry: (job: Job) => void }) {
  const [findings, setFindings] = useState<Finding[]>([]);
  const [isLoadingFindings, setIsLoadingFindings] = useState(false);
  const [findingsError, setFindingsError] = useState<string | null>(null);

  useEffect(() => {
    let isActive = true;

    async function loadFindings() {
      setIsLoadingFindings(true);
      setFindingsError(null);
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
          setFindingsError(err instanceof Error ? err.message : '加载 findings 失败');
        }
      } finally {
        if (isActive) {
          setIsLoadingFindings(false);
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
        <dt>Summary</dt><dd>{job.summary || '-'}</dd>
        <dt>错误</dt><dd className={job.error_message ? 'errorText' : undefined}>{job.error_message || '-'}</dd>
      </dl>

      {isRetryable(job) ? <div className="detailActions"><button onClick={() => onRetry(job)}>重新排队 review</button></div> : null}

      <section className="findingsSection">
        <div className="panelHeader compact"><h2>Findings</h2><span>{isLoadingFindings ? '加载中...' : `${findings.length} 条`}</span></div>
        {findingsError ? <div className="alert">{findingsError}</div> : null}
        {findings.length === 0 && !isLoadingFindings ? <p className="empty">暂无 findings</p> : null}
        <div className="findingList">
          {findings.map((finding) => (
            <article key={finding.id} className="finding">
              <div className="findingTopline"><strong>{finding.title}</strong><span className={`badge ${finding.severity}`}>{finding.severity}</span></div>
              <p>{finding.body}</p>
              <small>
                {finding.category} · {finding.path}{finding.line ? `:${finding.line}` : ''}
                {typeof finding.confidence === 'number' ? ` · confidence ${finding.confidence.toFixed(2)}` : ''}
                {finding.is_inline ? ` · inline ${finding.is_posted ? 'posted' : 'pending'}` : ''}
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
  return <span className={`badge ${status}`}>{status}</span>;
}

function isRetryable(job: Job) {
  return job.status === 'errored';
}

function shortSha(sha: string) {
  return sha ? sha.slice(0, 8) : '-';
}

function splitList(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean);
}

function normalizeSettings(settings: RuntimeSettings) {
  return {
    ...settings,
    review_max_diff_bytes: Number(settings.review_max_diff_bytes) || defaultSettings.review_max_diff_bytes,
    review_max_findings: Number(settings.review_max_findings) || defaultSettings.review_max_findings,
    review_exclude_paths: settings.review_exclude_paths,
  };
}

async function logout() {
  await fetch('/api/logout', { method: 'POST' });
}
