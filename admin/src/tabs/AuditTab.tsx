import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { Alert, Button, Input, Select, Space, Spin, Table, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import { useState } from 'react'
import type { V1AuditLogEntry } from '../playtesthubapi/generated-definitions/V1AuditLogEntry'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'
import { usePlaytesthubServiceAdminApi_GetAuditLog_ByPlaytestId } from '../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'

// Audit-action catalogue mirrors pkg/repo/auditlog_m{1,2,3}.go constants
// (schema.md §"AuditLog — `action` enum"). Used for the action filter dropdown.
const AUDIT_ACTIONS = [
  'nda.accept',
  'nda.edit',
  'playtest.edit',
  'playtest.soft_delete',
  'playtest.status_transition',
  'applicant.approve',
  'applicant.reject',
  'applicant.dm_sent',
  'applicant.dm_failed',
  'dm.circuit_opened',
  'dm.circuit_closed',
  'code.upload',
  'code.upload_rejected',
  'code.grant_orphaned',
  'campaign.create',
  'campaign.create_failed',
  'campaign.generate_codes',
  'campaign.generate_codes_failed',
  'survey.create',
  'survey.edit'
] as const

const ACTOR_SYSTEM = 'system'
const ACTOR_ALL = ''
const ACTOR_USER = '__user__'

type ParsedJson = { ok: true; value: unknown } | { ok: false; raw: string }

function parsePayload(raw: string | null | undefined): ParsedJson {
  if (!raw) return { ok: true, value: null }
  try {
    return { ok: true, value: JSON.parse(raw) }
  } catch {
    return { ok: false, raw }
  }
}

function pretty(v: unknown): string {
  return JSON.stringify(v, null, 2)
}

// Shallow object key diff. Audit payloads in schema.md are flat 1-level
// records, so a shallow comparison covers every documented shape; nested
// values render as embedded JSON within the changed-key row.
type DiffEntry = { key: string; before: unknown; after: unknown; status: 'added' | 'removed' | 'modified' | 'unchanged' }

function shallowDiff(beforeVal: unknown, afterVal: unknown): DiffEntry[] {
  const isObj = (v: unknown): v is Record<string, unknown> =>
    typeof v === 'object' && v !== null && !Array.isArray(v)
  if (!isObj(beforeVal) && !isObj(afterVal)) return []
  const before = isObj(beforeVal) ? beforeVal : {}
  const after = isObj(afterVal) ? afterVal : {}
  const keys = Array.from(new Set([...Object.keys(before), ...Object.keys(after)])).sort()
  return keys.map(key => {
    const inBefore = key in before
    const inAfter = key in after
    if (inBefore && !inAfter) return { key, before: before[key], after: undefined, status: 'removed' }
    if (!inBefore && inAfter) return { key, before: undefined, after: after[key], status: 'added' }
    const same = JSON.stringify(before[key]) === JSON.stringify(after[key])
    return { key, before: before[key], after: after[key], status: same ? 'unchanged' : 'modified' }
  })
}

function AuditDiff({ beforeJson, afterJson }: { beforeJson: string | null | undefined; afterJson: string | null | undefined }) {
  const beforeParsed = parsePayload(beforeJson)
  const afterParsed = parsePayload(afterJson)
  const diff = beforeParsed.ok && afterParsed.ok ? shallowDiff(beforeParsed.value, afterParsed.value) : []
  const changedKeys = diff.filter(d => d.status !== 'unchanged').map(d => d.key)

  return (
    <div data-testid="audit-diff" style={{ background: '#fafafa', padding: 12, borderRadius: 4 }}>
      {changedKeys.length > 0 && (
        <div style={{ marginBottom: 8 }}>
          <Typography.Text strong>Changed keys: </Typography.Text>
          {changedKeys.map(k => (
            <Tag key={k} color="gold" data-testid={`audit-diff-key-${k}`}>
              {k}
            </Tag>
          ))}
        </div>
      )}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
        <div>
          <Typography.Text type="secondary">Before</Typography.Text>
          <pre
            data-testid="audit-before"
            style={{ margin: 0, padding: 8, background: '#fff', border: '1px solid #f0f0f0', borderRadius: 4, maxHeight: 320, overflow: 'auto' }}>
            {beforeParsed.ok ? pretty(beforeParsed.value) : beforeParsed.raw}
          </pre>
        </div>
        <div>
          <Typography.Text type="secondary">After</Typography.Text>
          <pre
            data-testid="audit-after"
            style={{ margin: 0, padding: 8, background: '#fff', border: '1px solid #f0f0f0', borderRadius: 4, maxHeight: 320, overflow: 'auto' }}>
            {afterParsed.ok ? pretty(afterParsed.value) : afterParsed.raw}
          </pre>
        </div>
      </div>
    </div>
  )
}

export function AuditTab({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const playtestId = playtest.id ?? ''

  const [actorMode, setActorMode] = useState<typeof ACTOR_ALL | typeof ACTOR_SYSTEM | typeof ACTOR_USER>(ACTOR_ALL)
  const [actorUserInput, setActorUserInput] = useState('')
  const [appliedActorUserId, setAppliedActorUserId] = useState('')
  const [actionFilter, setActionFilter] = useState<string | undefined>(undefined)
  const [pageStack, setPageStack] = useState<string[]>([])
  const [currentToken, setCurrentToken] = useState<string>('')

  const actorFilter =
    actorMode === ACTOR_ALL
      ? undefined
      : actorMode === ACTOR_SYSTEM
        ? 'system'
        : appliedActorUserId.trim() || undefined

  const auditQuery = usePlaytesthubServiceAdminApi_GetAuditLog_ByPlaytestId(sdk, {
    playtestId,
    queryParams: {
      actorFilter,
      actionFilter,
      pageToken: currentToken || undefined,
      pageSize: 50
    }
  })
  const entries = (auditQuery.data?.entries ?? []) as V1AuditLogEntry[]
  const nextPageToken = auditQuery.data?.nextPageToken ?? ''

  const resetPagination = () => {
    setPageStack([])
    setCurrentToken('')
  }

  const onActorModeChange = (val: string) => {
    setActorMode(val as typeof actorMode)
    if (val !== ACTOR_USER) {
      setActorUserInput('')
      setAppliedActorUserId('')
    }
    resetPagination()
  }

  const onActionFilterChange = (val: string | undefined) => {
    setActionFilter(val ?? undefined)
    resetPagination()
  }

  const commitActorUserInput = () => {
    setAppliedActorUserId(actorUserInput)
    resetPagination()
  }

  const onNext = () => {
    if (!nextPageToken) return
    setPageStack(prev => [...prev, currentToken])
    setCurrentToken(nextPageToken)
  }

  const onPrev = () => {
    if (pageStack.length === 0) return
    const prev = [...pageStack]
    const back = prev.pop() ?? ''
    setPageStack(prev)
    setCurrentToken(back)
  }

  const columns = [
    {
      title: 'Time',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 180,
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '—')
    },
    {
      title: 'Actor',
      dataIndex: 'actorUserId',
      key: 'actorUserId',
      width: 220,
      render: (v: string | null | undefined) =>
        v ? <Typography.Text code>{v}</Typography.Text> : <Tag color="default">system</Tag>
    },
    {
      title: 'Action',
      dataIndex: 'action',
      key: 'action',
      width: 220,
      render: (v: string | null | undefined) => <Tag color="blue">{v ?? '—'}</Tag>
    }
  ]

  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="audit-tab">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <Typography.Text type="secondary">Read-only. System-emitted rows show actor as “system”.</Typography.Text>
        <Button onClick={() => auditQuery.refetch()}>Refresh</Button>
      </div>

      <Space wrap>
        <Select
          aria-label="Actor filter"
          value={actorMode}
          onChange={onActorModeChange}
          style={{ width: 200 }}
          options={[
            { value: ACTOR_ALL, label: 'All actors' },
            { value: ACTOR_SYSTEM, label: 'System' },
            { value: ACTOR_USER, label: 'Admin user…' }
          ]}
        />
        {actorMode === ACTOR_USER && (
          <Input
            aria-label="Actor user id"
            placeholder="Admin user id (UUID), press Enter"
            value={actorUserInput}
            onChange={e => setActorUserInput(e.target.value)}
            onBlur={commitActorUserInput}
            onPressEnter={commitActorUserInput}
            style={{ width: 320 }}
          />
        )}
        <Select
          aria-label="Action filter"
          allowClear
          placeholder="All actions"
          value={actionFilter}
          onChange={onActionFilterChange}
          style={{ width: 280 }}
          options={AUDIT_ACTIONS.map(a => ({ value: a, label: a }))}
        />
      </Space>

      {auditQuery.isLoading && <Spin description="Loading audit log..." />}
      {auditQuery.error && (
        <Alert
          type="error"
          message="Failed to load audit log."
          action={
            <Button size="small" onClick={() => auditQuery.refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!auditQuery.isLoading && !auditQuery.error && (
        <>
          <Table<V1AuditLogEntry>
            rowKey={row => row.id ?? ''}
            dataSource={entries}
            columns={columns}
            pagination={false}
            expandable={{
              expandedRowRender: row => <AuditDiff beforeJson={row.beforeJson} afterJson={row.afterJson} />,
              rowExpandable: () => true
            }}
          />
          <div style={{ marginTop: 16, display: 'flex', justifyContent: 'space-between' }}>
            <Typography.Text type="secondary">
              {entries.length} row(s){pageStack.length > 0 ? ` · page ${pageStack.length + 1}` : ''}
            </Typography.Text>
            <Space>
              <Button onClick={onPrev} disabled={pageStack.length === 0}>
                Previous
              </Button>
              <Button onClick={onNext} disabled={!nextPageToken}>
                Next
              </Button>
            </Space>
          </div>
        </>
      )}
    </Space>
  )
}
