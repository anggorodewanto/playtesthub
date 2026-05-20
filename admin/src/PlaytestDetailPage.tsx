/**
 * PlaytestDetailPage — M5.C admin shell.
 *
 * Renders the list+detail page model documented in docs/STATUS_M5.md D5 +
 * PRD §5.7 M5.C restructure. Top header carries the breadcrumb + title +
 * date range + status pill + Publish/Stop verbs + share link. Below sit
 * four tabs whose selected key is persisted to the `?tab=` query param.
 *
 * Publish (DRAFT→OPEN) + Stop Playtest (OPEN→CLOSED) are pure UI copy
 * renames over M4's existing TransitionPlaytestStatus RPC — no
 * state-machine change.
 */

import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Breadcrumb,
  Button,
  Modal,
  Space,
  Spin,
  Tabs,
  Tag,
  Typography,
  message
} from 'antd'
import dayjs from 'dayjs'
import { useMemo } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router'

import type { V1Playtest } from './playtesthubapi/generated-definitions/V1Playtest'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation,
  usePlaytesthubServiceAdminApi_GetPlaytests
} from './playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'
import { DistributionTab } from './tabs/DistributionTab'
import { ParticipantsTab } from './tabs/ParticipantsTab'
import { DiscordBotToolsTab } from './tabs/DiscordBotToolsTab'

const PlaytestStatus = {
  DRAFT: 'PLAYTEST_STATUS_DRAFT',
  OPEN: 'PLAYTEST_STATUS_OPEN',
  CLOSED: 'PLAYTEST_STATUS_CLOSED'
} as const

const STATUS_PILL: Record<string, { text: string; color: string }> = {
  [PlaytestStatus.DRAFT]: { text: 'Draft', color: 'default' },
  [PlaytestStatus.OPEN]: { text: 'Published', color: 'green' },
  [PlaytestStatus.CLOSED]: { text: 'Closed', color: 'red' }
}

const TABS = ['info', 'distribution', 'participants', 'bot-tools'] as const
type TabKey = (typeof TABS)[number]

function isTabKey(v: string): v is TabKey {
  return (TABS as readonly string[]).includes(v)
}

export function PlaytestDetailPage() {
  const { slug } = useParams<{ slug: string }>()
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()

  const tabParam = searchParams.get('tab') ?? 'info'
  const activeTab: TabKey = isTabKey(tabParam) ? tabParam : 'info'

  const { data, isLoading, error, refetch } = usePlaytesthubServiceAdminApi_GetPlaytests(sdk, {})
  const playtest = useMemo<V1Playtest | undefined>(() => {
    const rows = (data?.playtests ?? []) as V1Playtest[]
    return rows.find(p => p.slug === slug)
  }, [data, slug])

  const transitionMutation = usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation(sdk, {
    onSuccess: () => {
      message.success('Status updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
    },
    onError: (err: { response?: { data?: { errorMessage?: string } } }) =>
      message.error(err?.response?.data?.errorMessage ?? 'Failed to update status')
  })

  if (isLoading) return <Spin size="large" data-testid="playtest-detail-loading" />
  if (error || !data) {
    return (
      <Alert
        type="error"
        message="Failed to load playtest"
        action={
          <Button size="small" onClick={() => refetch()}>
            Retry
          </Button>
        }
      />
    )
  }
  if (!playtest) {
    return (
      <Alert
        type="warning"
        message={`Playtest "${slug}" not found`}
        action={
          <Button size="small" onClick={() => navigate('/')}>
            Back to playtests
          </Button>
        }
      />
    )
  }

  const status = playtest.status ?? ''
  const pill = STATUS_PILL[status] ?? { text: status || '—', color: 'default' }
  const isDraft = status === PlaytestStatus.DRAFT
  const isOpen = status === PlaytestStatus.OPEN

  const handleTabChange = (key: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('tab', key)
    setSearchParams(next, { replace: true })
  }

  const publish = () => {
    Modal.confirm({
      title: 'Publish this playtest?',
      content: 'Players can sign up once published.',
      okText: 'Publish',
      onOk: () =>
        new Promise<void>((resolve, reject) => {
          transitionMutation.mutate(
            { playtestId: playtest.id ?? '', data: { targetStatus: PlaytestStatus.OPEN } },
            { onSuccess: () => resolve(), onError: () => reject() }
          )
        })
    })
  }

  const stop = () => {
    Modal.confirm({
      title: 'Stop this playtest?',
      content:
        'Stopping this playtest will close it for new sign-ups. Approved players keep access to their codes / builds.',
      okText: 'Stop Playtest',
      okButtonProps: { danger: true },
      onOk: () =>
        new Promise<void>((resolve, reject) => {
          transitionMutation.mutate(
            { playtestId: playtest.id ?? '', data: { targetStatus: PlaytestStatus.CLOSED } },
            { onSuccess: () => resolve(), onError: () => reject() }
          )
        })
    })
  }

  const copyShareLink = () => {
    const link = `${window.location.origin}/#/playtest/${playtest.slug ?? ''}`
    void navigator.clipboard?.writeText(link).then(
      () => message.success('Playtest link copied'),
      () => message.error('Clipboard unavailable')
    )
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="playtest-detail-page">
      <Breadcrumb
        items={[
          { title: <a onClick={() => navigate('/')}>Playtests</a> },
          { title: playtest.title ?? playtest.slug ?? '—' }
        ]}
      />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            {playtest.title ?? '—'}
          </Typography.Title>
          <Space style={{ marginTop: 4 }}>
            <Tag color={pill.color} data-testid="playtest-status-pill">
              {pill.text}
            </Tag>
            <Typography.Text type="secondary">{formatDateRange(playtest.startsAt, playtest.endsAt)}</Typography.Text>
          </Space>
        </div>
        <Space wrap data-testid="playtest-header-actions">
          <Button onClick={copyShareLink}>Copy share link</Button>
          {isDraft && (
            <Button type="primary" onClick={publish} data-testid="header-publish">
              Publish
            </Button>
          )}
          {isOpen && (
            <Button danger onClick={stop} data-testid="header-stop">
              Stop Playtest
            </Button>
          )}
        </Space>
      </div>

      <Tabs
        activeKey={activeTab}
        onChange={handleTabChange}
        items={[
          { key: 'info', label: 'Playtest Info', children: <PlaytestInfoTab playtest={playtest} /> },
          {
            key: 'distribution',
            label: 'Distribution',
            children: <DistributionTab playtest={playtest} />
          },
          { key: 'participants', label: 'Participants', children: <ParticipantsTab playtest={playtest} /> },
          { key: 'bot-tools', label: 'Discord Bot Tools', children: <DiscordBotToolsTab playtest={playtest} /> }
        ]}
      />
    </Space>
  )
}

function PlaytestInfoTab({ playtest }: { playtest: V1Playtest }) {
  const navigate = useNavigate()

  const rows: Array<[string, React.ReactNode]> = [
    ['Title', playtest.title ?? '—'],
    ['Slug', playtest.slug ?? '—'],
    ['Description', playtest.description ?? '—'],
    ['Banner Image', playtest.bannerImageUrl ?? '—'],
    ['Start Date', playtest.startsAt ? dayjs(playtest.startsAt).format('YYYY-MM-DD HH:mm Z') : '—'],
    ['End Date', playtest.endsAt ? dayjs(playtest.endsAt).format('YYYY-MM-DD HH:mm Z') : '—'],
    ['Platforms', (playtest.platforms ?? []).join(', ') || '—'],
    ['NDA Required', playtest.ndaRequired ? 'Yes' : 'No'],
    ['Distribution Model', playtest.distributionModel ?? '—'],
    ['Approval Method', playtest.autoApprove ? `Auto-approve (limit ${playtest.autoApproveLimit ?? '—'})` : 'Manual'],
    ['Max Participants', playtest.autoApproveLimit ?? '—']
  ]

  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="playtest-info-tab">
      <table style={{ borderCollapse: 'collapse', width: '100%' }}>
        <tbody>
          {rows.map(([label, value]) => (
            <tr key={label}>
              <td style={{ padding: '8px 12px', fontWeight: 500, width: 220, verticalAlign: 'top' }}>{label}</td>
              <td style={{ padding: '8px 12px' }}>{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <Button onClick={() => navigate(`/${playtest.id ?? ''}/edit`)} data-testid="playtest-info-edit">
        Edit
      </Button>
    </Space>
  )
}

function formatDateRange(starts?: string | null, ends?: string | null): string {
  if (!starts && !ends) return 'No dates set'
  const s = starts ? dayjs(starts).format('MMM D, YYYY') : '—'
  const e = ends ? dayjs(ends).format('MMM D, YYYY') : '—'
  return `${s} → ${e}`
}
