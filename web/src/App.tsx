import { useEffect, useState } from 'react';

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

type JobsResponse = {
  jobs: Job[];
};

type FindingsResponse = {
  findings: Finding[];
};

export function App() {
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
    <main className="page">
      <header className="header">
        <div>
          <p className="eyebrow">Gitea PR Code Review Bot</p>
          <h1>Review Jobs</h1>
        </div>
        <button onClick={() => void loadJobs()} disabled={isLoading}>
          {isLoading ? '刷新中...' : '刷新'}
        </button>
      </header>

      {error ? <div className="alert">{error}</div> : null}

      <section className="layout">
        <div className="panel">
          <div className="panelHeader">
            <h2>任务列表</h2>
            <span>{jobs.length} 条</span>
          </div>
          <div className="jobList">
            {jobs.length === 0 && !isLoading ? <p className="empty">暂无 review job</p> : null}
            {jobs.map((job) => (
              <button
                key={job.id}
                className={job.id === selectedJob?.id ? 'job active' : 'job'}
                onClick={() => setSelectedJob(job)}
              >
                <div className="jobTopline">
                  <strong>#{job.pr_number}</strong>
                  <StatusBadge status={job.status} />
                </div>
                <span>{job.repo_full_name}</span>
                <small>{shortSha(job.head_sha)} · attempts {job.attempt_count}</small>
              </button>
            ))}
          </div>
        </div>

        <div className="panel detail">
          <div className="panelHeader">
            <h2>任务详情</h2>
          </div>
          {selectedJob ? (
            <JobDetail
              job={selectedJob}
              onRetry={(job) => retryJob(job).catch((err) => setError(err instanceof Error ? err.message : '重试失败'))}
            />
          ) : (
            <p className="empty">请选择一个任务</p>
          )}
        </div>
      </section>
    </main>
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
        <dt>ID</dt>
        <dd>{job.id}</dd>

        <dt>状态</dt>
        <dd><StatusBadge status={job.status} /></dd>

        <dt>尝试次数</dt>
        <dd>{job.attempt_count}</dd>

        <dt>仓库</dt>
        <dd>{job.repo_full_name}</dd>

        <dt>PR</dt>
        <dd>#{job.pr_number}</dd>

        <dt>事件</dt>
        <dd>{job.event_name} / {job.action || '-'}</dd>

        <dt>提交</dt>
        <dd><code>{job.head_sha}</code></dd>

        <dt>触发人</dt>
        <dd>{job.sender || '-'}</dd>

        <dt>创建时间</dt>
        <dd>{new Date(job.created_at).toLocaleString()}</dd>

        <dt>Summary</dt>
        <dd>{job.summary || '-'}</dd>

        <dt>错误</dt>
        <dd className={job.error_message ? 'errorText' : undefined}>{job.error_message || '-'}</dd>
      </dl>

      {isRetryable(job) ? (
        <div className="detailActions">
          <button onClick={() => onRetry(job)}>重新排队 review</button>
        </div>
      ) : null}

      <section className="findingsSection">
        <div className="panelHeader compact">
          <h2>Findings</h2>
          <span>{isLoadingFindings ? '加载中...' : `${findings.length} 条`}</span>
        </div>
        {findingsError ? <div className="alert">{findingsError}</div> : null}
        {findings.length === 0 && !isLoadingFindings ? <p className="empty">暂无 findings</p> : null}
        <div className="findingList">
          {findings.map((finding) => (
            <article key={finding.id} className="finding">
              <div className="findingTopline">
                <strong>{finding.title}</strong>
                <span className={`badge ${finding.severity}`}>{finding.severity}</span>
              </div>
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
