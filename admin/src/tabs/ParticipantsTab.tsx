import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Checkbox,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message
} from 'antd'
import dayjs from 'dayjs'
import { useMemo, useState } from 'react'
import type { V1Applicant } from '../playtesthubapi/generated-definitions/V1Applicant'
import type { V1CodePoolStats } from '../playtesthubapi/generated-definitions/V1CodePoolStats'
import type { V1ParticipantRow } from '../playtesthubapi/generated-definitions/V1ParticipantRow'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation,
  usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetParticipants_ByPlaytestId
} from '../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'
import { ApplicantStatus, type ApplicantStatusValue, DmStatus } from '../shared/playtesthub-enums'
import { toastError } from '../shared/api-error'

const STATUS_TAG: Record<string, { text: string; color: string }> = {
  [ApplicantStatus.PENDING]: { text: 'Pending', color: 'blue' },
  [ApplicantStatus.APPROVED]: { text: 'Approved', color: 'green' },
  [ApplicantStatus.REJECTED]: { text: 'Rejected', color: 'red' }
}

const DM_STATUS_TAG: Record<string, { text: string; color: string }> = {
  DM_STATUS_PENDING: { text: 'Pending', color: 'default' },
  [DmStatus.SENT]: { text: 'Sent', color: 'green' },
  [DmStatus.FAILED]: { text: 'Failed', color: 'red' }
}

const POOL_LOW_RATIO = 0.1

function isPoolLow(stats: V1CodePoolStats | null | undefined): boolean {
  const total = stats?.total ?? 0
  const unused = stats?.unused ?? 0
  if (total <= 0) return false
  return unused / total <= POOL_LOW_RATIO
}

function LowPoolBanner({ stats }: { stats: V1CodePoolStats | null | undefined }) {
  if (!isPoolLow(stats)) return null
  return (
    <Alert
      type="warning"
      showIcon
      message="Code pool is low"
      description={`Only ${stats?.unused ?? 0} of ${stats?.total ?? 0} codes remain unused. Top up before approving more applicants.`}
      style={{ marginBottom: 16 }}
    />
  )
}

type MergedRow = V1ParticipantRow & {
  platforms?: string[] | null
  lastDmStatus?: string | null
  lastDmError?: string | null
}

export function ParticipantsTab({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const queryClient = useQueryClient()
  const playtestId = playtest.id ?? ''

  const [statusFilter, setStatusFilter] = useState<ApplicantStatusValue | ''>('')
  const [dmFailedOnly, setDmFailedOnly] = useState(false)
  const [rejectTarget, setRejectTarget] = useState<MergedRow | null>(null)
  const [rejectReason, setRejectReason] = useState('')

  const participantsQuery = usePlaytesthubServiceAdminApi_GetParticipants_ByPlaytestId(sdk, {
    playtestId,
    queryParams: statusFilter ? { statusFilter } : undefined
  })

  const applicantsQuery = usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId(sdk, {
    playtestId,
    queryParams: {
      statusFilter: (statusFilter || ApplicantStatus.UNSPECIFIED) as ApplicantStatusValue,
      dmFailedFilter: dmFailedOnly
    }
  })

  const codesQuery = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(sdk, { playtestId })

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Participants_ByPlaytestId] })
    queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Applicants_ByPlaytestId] })
    queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId] })
  }

  const approveMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation(sdk, {
    onSuccess: () => {
      message.success('Applicant approved')
      invalidate()
    },
    onError: toastError('approve')
  })
  const rejectMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation(sdk, {
    onSuccess: () => {
      message.success('Applicant rejected')
      setRejectTarget(null)
      setRejectReason('')
      invalidate()
    },
    onError: toastError('reject')
  })
  const retryDmMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation(sdk, {
    onSuccess: () => {
      message.success('Retry DM enqueued')
      invalidate()
    },
    onError: toastError('retry DM')
  })

  const participants = (participantsQuery.data?.participants ?? []) as V1ParticipantRow[]
  const applicants = (applicantsQuery.data?.applicants ?? []) as V1Applicant[]
  const applicantById = useMemo(() => {
    const map = new Map<string, V1Applicant>()
    for (const a of applicants) {
      if (a.id) map.set(a.id, a)
    }
    return map
  }, [applicants])

  const rows = useMemo<MergedRow[]>(() => {
    const merged: MergedRow[] = participants.map(p => {
      const a = applicantById.get(p.applicantId ?? '')
      return {
        ...p,
        platforms: a?.platforms ?? null,
        lastDmStatus: a?.lastDmStatus ?? null,
        lastDmError: a?.lastDmError ?? null
      }
    })
    if (!dmFailedOnly) return merged
    return merged.filter(r => r.lastDmStatus === DmStatus.FAILED)
  }, [participants, applicantById, dmFailedOnly])

  const cap = playtest.autoApproveLimit ?? null
  const enrolled = rows.length

  const columns = [
    { title: 'Discord Handle', dataIndex: 'discordHandle', key: 'discordHandle', render: (v: string) => v || '—' },
    { title: 'AGS User ID', dataIndex: 'userId', key: 'userId', render: (v: string) => v || '—' },
    {
      title: 'Platforms',
      dataIndex: 'platforms',
      key: 'platforms',
      render: (v: string[] | null | undefined) =>
        (v ?? []).map(p => p.replace('PLATFORM_', '').toLowerCase()).join(', ') || '—'
    },
    {
      title: 'Sign-up Date',
      dataIndex: 'signupAt',
      key: 'signupAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD') : '—')
    },
    {
      title: 'NDA Accepted',
      dataIndex: 'ndaAcceptedAt',
      key: 'ndaAcceptedAt',
      render: (v: string | null | undefined) => (v ? '✓' : '—')
    },
    {
      title: 'Code Sent Date',
      dataIndex: 'codeSentAt',
      key: 'codeSentAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD') : '—')
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (v: string) => {
        const tag = STATUS_TAG[v] ?? { text: v ?? '—', color: 'default' }
        return <Tag color={tag.color}>{tag.text}</Tag>
      }
    },
    {
      title: 'DM',
      dataIndex: 'lastDmStatus',
      key: 'lastDmStatus',
      render: (v: string | null | undefined, row: MergedRow) => {
        if (!v) return <Tag>—</Tag>
        const info = DM_STATUS_TAG[v] ?? { text: v, color: 'default' }
        return (
          <Tag color={info.color} title={row.lastDmError ?? undefined}>
            {info.text}
          </Tag>
        )
      }
    },
    {
      title: 'Action',
      key: 'action',
      render: (_: unknown, row: MergedRow) => {
        const isPending = row.status === ApplicantStatus.PENDING
        const canRetryDm = row.status === ApplicantStatus.APPROVED && row.lastDmStatus === DmStatus.FAILED
        if (!isPending && !canRetryDm) return null
        return (
          <Space wrap>
            {isPending && (
              <Popconfirm
                title="Approve this applicant?"
                description="A code will be reserved and granted from the pool."
                okText="Approve"
                onConfirm={() =>
                  approveMutation.mutate({ applicantId: row.applicantId ?? '', data: {} })
                }>
                <Button size="small" type="primary">
                  Approve
                </Button>
              </Popconfirm>
            )}
            {isPending && (
              <Button size="small" danger onClick={() => setRejectTarget(row)}>
                Reject
              </Button>
            )}
            {canRetryDm && (
              <Button
                size="small"
                onClick={() => retryDmMutation.mutate({ applicantId: row.applicantId ?? '', data: {} })}>
                Retry DM
              </Button>
            )}
          </Space>
        )
      }
    }
  ]

  if (participantsQuery.error) {
    return (
      <Alert
        type="error"
        message="Failed to load participants"
        action={
          <Button size="small" onClick={() => participantsQuery.refetch()}>
            Retry
          </Button>
        }
      />
    )
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="participants-tab">
      <LowPoolBanner stats={codesQuery.data?.stats} />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 16 }}>
        <Typography.Text>
          {enrolled} / {cap ?? '∞'} enrolled
        </Typography.Text>
        <Space wrap>
          <Select
            allowClear
            placeholder="Filter by status"
            style={{ width: 200 }}
            value={statusFilter || undefined}
            onChange={v => setStatusFilter((v ?? '') as ApplicantStatusValue | '')}
            options={[
              { value: ApplicantStatus.PENDING, label: 'Pending' },
              { value: ApplicantStatus.APPROVED, label: 'Approved' },
              { value: ApplicantStatus.REJECTED, label: 'Rejected' }
            ]}
            data-testid="participants-status-filter"
          />
          <Checkbox checked={dmFailedOnly} onChange={e => setDmFailedOnly(e.target.checked)}>
            DM failed only
          </Checkbox>
        </Space>
      </div>
      <Table<MergedRow>
        rowKey={row => row.applicantId ?? ''}
        loading={participantsQuery.isLoading}
        dataSource={rows}
        columns={columns}
        pagination={{ pageSize: 25 }}
      />
      <Modal
        title="Reject applicant"
        open={!!rejectTarget}
        onCancel={() => {
          setRejectTarget(null)
          setRejectReason('')
        }}
        onOk={() =>
          rejectTarget &&
          rejectMutation.mutate({
            applicantId: rejectTarget.applicantId ?? '',
            data: { rejectionReason: rejectReason.trim() || undefined }
          })
        }
        okText="Reject"
        okButtonProps={{ danger: true, loading: rejectMutation.isPending }}>
        <Typography.Paragraph>
          Reject <strong>{rejectTarget?.discordHandle ?? rejectTarget?.userId ?? 'applicant'}</strong>? This is terminal — they cannot be re-approved.
        </Typography.Paragraph>
        <Form.Item label="Reason (optional)" style={{ marginBottom: 0 }}>
          <Input.TextArea
            rows={3}
            value={rejectReason}
            maxLength={500}
            onChange={e => setRejectReason(e.target.value)}
            placeholder="Stored on the applicant row for your future reference. Not shown to the player."
          />
        </Form.Item>
      </Modal>
    </Space>
  )
}
