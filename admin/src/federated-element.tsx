import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Checkbox,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Typography,
  Upload,
  message
} from 'antd'
import type { UploadFile } from 'antd/es/upload/interface'
import dayjs, { type Dayjs } from 'dayjs'
import { useEffect, useMemo, useState } from 'react'
import { Route, Routes, useNavigate, useParams } from 'react-router'
import type { V1Applicant } from './playtesthubapi/generated-definitions/V1Applicant'
import type { V1Code } from './playtesthubapi/generated-definitions/V1Code'
import type { V1CodePoolStats } from './playtesthubapi/generated-definitions/V1CodePoolStats'
import type { V1Playtest } from './playtesthubapi/generated-definitions/V1Playtest'
import type { V1UploadCodesRejection } from './playtesthubapi/generated-definitions/V1UploadCodesRejection'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation,
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation,
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytestMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation,
  usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId,
  usePlaytesthubServiceAdminApi_GetPlaytests,
  usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation
} from './playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'

const PLATFORMS = [
  { value: 'PLATFORM_STEAM', label: 'Steam' },
  { value: 'PLATFORM_XBOX', label: 'Xbox' },
  { value: 'PLATFORM_PLAYSTATION', label: 'PlayStation' },
  { value: 'PLATFORM_EPIC', label: 'Epic' },
  { value: 'PLATFORM_OTHER', label: 'Other' }
] as const

const STATUS_TAG: Record<string, { text: string; color: string }> = {
  PLAYTEST_STATUS_DRAFT: { text: 'Draft', color: 'default' },
  PLAYTEST_STATUS_OPEN: { text: 'Open', color: 'green' },
  PLAYTEST_STATUS_CLOSED: { text: 'Closed', color: 'red' }
}

const DISTRIBUTION_LABEL: Record<string, string> = {
  DISTRIBUTION_MODEL_STEAM_KEYS: 'Steam keys',
  DISTRIBUTION_MODEL_AGS_CAMPAIGN: 'AGS Campaign'
}

export function FederatedElement() {
  return (
    <div style={{ padding: 16 }}>
      <Routes>
        <Route path="/" index element={<PlaytestsListPage />} />
        <Route path="new" element={<PlaytestCreatePage />} />
        <Route path=":playtestId/edit" element={<PlaytestEditPage />} />
        <Route path=":playtestId/applicants" element={<ApplicantsPage />} />
        <Route path=":playtestId/codes" element={<CodePoolPage />} />
      </Routes>
    </div>
  )
}

function PlaytestsListPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data, isLoading, error, refetch } = usePlaytesthubServiceAdminApi_GetPlaytests(sdk, {})
  const deleteMutation = usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Playtest deleted')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to delete')
  })
  const transitionMutation = usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation(sdk, {
    onSuccess: () => {
      message.success('Status updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to update status')
  })

  const playtests = (data?.playtests ?? []) as V1Playtest[]

  const columns = [
    { title: 'Slug', dataIndex: 'slug', key: 'slug' },
    { title: 'Title', dataIndex: 'title', key: 'title' },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (value: string | null | undefined) => {
        const info = STATUS_TAG[value ?? ''] ?? { text: value ?? '—', color: 'default' }
        return <Tag color={info.color}>{info.text}</Tag>
      }
    },
    {
      title: 'Distribution',
      dataIndex: 'distributionModel',
      key: 'distributionModel',
      render: (value: string | null | undefined) => DISTRIBUTION_LABEL[value ?? ''] ?? value ?? '—'
    },
    {
      title: 'Created',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (value: string | null | undefined) => (value ? dayjs(value).format('YYYY-MM-DD HH:mm') : '—')
    },
    {
      title: 'Actions',
      key: 'actions',
      render: (_: unknown, row: V1Playtest) => {
        const isDraft = row.status === 'PLAYTEST_STATUS_DRAFT'
        const isOpen = row.status === 'PLAYTEST_STATUS_OPEN'
        return (
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <Button size="small" onClick={() => navigate(`${row.id}/edit`)}>
              Edit
            </Button>
            <Button size="small" onClick={() => navigate(`${row.id}/applicants`)}>
              Applicants
            </Button>
            <Button size="small" onClick={() => navigate(`${row.id}/codes`)}>
              Codes
            </Button>
            {isDraft && (
              <Popconfirm
                title="Publish this playtest?"
                description="Players will be able to see it and sign up."
                okText="Publish"
                onConfirm={() =>
                  transitionMutation.mutate({
                    playtestId: row.id ?? '',
                    data: { targetStatus: 'PLAYTEST_STATUS_OPEN' }
                  })
                }>
                <Button size="small" type="primary">
                  Publish
                </Button>
              </Popconfirm>
            )}
            {isOpen && (
              <Popconfirm
                title="Close this playtest?"
                description="Players can no longer sign up. Existing applicants keep their state."
                okText="Close"
                okButtonProps={{ danger: true }}
                onConfirm={() =>
                  transitionMutation.mutate({
                    playtestId: row.id ?? '',
                    data: { targetStatus: 'PLAYTEST_STATUS_CLOSED' }
                  })
                }>
                <Button size="small" danger>
                  Close
                </Button>
              </Popconfirm>
            )}
            <Popconfirm
              title="Soft-delete this playtest?"
              description="Row will be hidden from players. Applicants + codes are preserved."
              okText="Delete"
              okButtonProps={{ danger: true }}
              onConfirm={() => deleteMutation.mutate({ playtestId: row.id ?? '' })}>
              <Button size="small" danger>
                Delete
              </Button>
            </Popconfirm>
          </div>
        )
      }
    }
  ]

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            Playtests
          </Typography.Title>
          <Typography.Text type="secondary">Create, edit, and soft-delete playtests in this namespace.</Typography.Text>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <Button onClick={() => refetch()}>Refresh</Button>
          <Button type="primary" onClick={() => navigate('new')}>
            New playtest
          </Button>
        </div>
      </div>

      {isLoading && <Spin description="Loading playtests..." />}
      {error && (
        <Alert
          type="error"
          message="Failed to load playtests."
          action={
            <Button size="small" onClick={() => refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!isLoading && !error && (
        <Table<V1Playtest>
          rowKey={row => row.id ?? row.slug ?? ''}
          dataSource={playtests}
          columns={columns}
          pagination={{ pageSize: 20 }}
        />
      )}
    </>
  )
}

type FormValues = {
  slug?: string
  title: string
  description?: string
  bannerImageUrl?: string
  platforms: string[]
  dateRange?: [Dayjs, Dayjs]
  ndaRequired?: boolean
  ndaText?: string
  distributionModel: string
  initialCodeQuantity?: number
}

function PlaytestCreatePage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [form] = Form.useForm<FormValues>()

  const createMutation = usePlaytesthubServiceAdminApi_CreatePlaytestMutation(sdk, {
    onSuccess: () => {
      message.success('Playtest created')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
      navigate('/')
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to create')
  })

  const handleSubmit = (values: FormValues) => {
    createMutation.mutate({
      data: {
        slug: values.slug,
        title: values.title,
        description: values.description,
        bannerImageUrl: values.bannerImageUrl,
        platforms: values.platforms,
        startsAt: values.dateRange?.[0].toISOString(),
        endsAt: values.dateRange?.[1].toISOString(),
        ndaRequired: values.ndaRequired,
        ndaText: values.ndaText,
        distributionModel: values.distributionModel,
        initialCodeQuantity: values.initialCodeQuantity
      }
    })
  }

  return (
    <>
      <Typography.Title level={2}>New playtest</Typography.Title>
      <Form<FormValues>
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        initialValues={{
          platforms: [],
          ndaRequired: false,
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS'
        }}>
        <Form.Item label="Slug" name="slug" rules={[{ required: true, message: 'Slug is required' }]}>
          <Input placeholder="e.g. summer-alpha-2026" />
        </Form.Item>
        <Form.Item label="Title" name="title" rules={[{ required: true, message: 'Title is required' }]}>
          <Input maxLength={200} />
        </Form.Item>
        <Form.Item label="Description" name="description">
          <Input.TextArea rows={4} maxLength={10000} />
        </Form.Item>
        <Form.Item
          label="Banner image URL"
          name="bannerImageUrl"
          rules={[{ type: 'url', message: 'Must be a URL' }]}
          extra="https only — backend rejects http.">
          <Input placeholder="https://..." />
        </Form.Item>
        <Form.Item label="Platforms" name="platforms" rules={[{ required: true, message: 'Pick at least one' }]}>
          <Select mode="multiple" options={PLATFORMS.map(p => ({ value: p.value, label: p.label }))} />
        </Form.Item>
        <Form.Item label="Starts / Ends (display-only in MVP)" name="dateRange">
          <DatePicker.RangePicker showTime style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="ndaRequired" valuePropName="checked">
          <Checkbox>Require NDA</Checkbox>
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev: FormValues, next: FormValues) => prev.ndaRequired !== next.ndaRequired}>
          {({ getFieldValue }) =>
            getFieldValue('ndaRequired') && (
              <Form.Item label="NDA text" name="ndaText" rules={[{ required: true, message: 'NDA text required when NDA enabled' }]}>
                <Input.TextArea rows={4} />
              </Form.Item>
            )
          }
        </Form.Item>
        <Form.Item label="Distribution model" name="distributionModel" rules={[{ required: true }]}>
          <Radio.Group>
            <Radio value="DISTRIBUTION_MODEL_STEAM_KEYS">Steam keys</Radio>
            <Radio value="DISTRIBUTION_MODEL_AGS_CAMPAIGN">AGS Campaign</Radio>
          </Radio.Group>
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev: FormValues, next: FormValues) => prev.distributionModel !== next.distributionModel}>
          {({ getFieldValue }) =>
            getFieldValue('distributionModel') === 'DISTRIBUTION_MODEL_AGS_CAMPAIGN' && (
              <Form.Item label="Initial code quantity" name="initialCodeQuantity" rules={[{ type: 'number', min: 1, max: 50000 }]}>
                <InputNumber min={1} max={50000} style={{ width: '100%' }} />
              </Form.Item>
            )
          }
        </Form.Item>
        {createMutation.isError && (
          <Form.Item>
            <Alert type="error" message={createMutation.error?.response?.data?.errorMessage ?? 'Create failed'} />
          </Form.Item>
        )}
        <Form.Item style={{ marginBottom: 0 }}>
          <div style={{ display: 'flex', gap: 8 }}>
            <Button onClick={() => navigate('/')}>Cancel</Button>
            <Button type="primary" htmlType="submit" loading={createMutation.isPending}>
              Create
            </Button>
          </div>
        </Form.Item>
      </Form>
    </>
  )
}

function PlaytestEditPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { playtestId = '' } = useParams()
  const [form] = Form.useForm<FormValues>()

  const { data, isLoading, error } = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )

  const editMutation = usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Playtest updated')
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtests] })
      queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Playtest_ByPlaytestId] })
      navigate('/')
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to update')
  })

  const playtest = data?.playtest as V1Playtest | undefined

  const initialValues = useMemo<Partial<FormValues>>(() => {
    if (!playtest) return {}
    const start = playtest.startsAt ? dayjs(playtest.startsAt) : undefined
    const end = playtest.endsAt ? dayjs(playtest.endsAt) : undefined
    return {
      title: playtest.title ?? '',
      description: playtest.description ?? undefined,
      bannerImageUrl: playtest.bannerImageUrl ?? undefined,
      platforms: (playtest.platforms ?? []) as string[],
      dateRange: start && end ? [start, end] : undefined,
      ndaRequired: playtest.ndaRequired ?? false,
      ndaText: playtest.ndaText ?? undefined,
      distributionModel: (playtest.distributionModel as string | undefined) ?? 'DISTRIBUTION_MODEL_STEAM_KEYS'
    }
  }, [playtest])

  useEffect(() => {
    if (playtest) form.setFieldsValue(initialValues as FormValues)
  }, [form, initialValues, playtest])

  const handleSubmit = (values: FormValues) => {
    editMutation.mutate({
      playtestId,
      data: {
        title: values.title,
        description: values.description,
        bannerImageUrl: values.bannerImageUrl,
        platforms: values.platforms,
        startsAt: values.dateRange?.[0].toISOString(),
        endsAt: values.dateRange?.[1].toISOString(),
        ndaRequired: values.ndaRequired,
        ndaText: values.ndaText
      }
    })
  }

  if (isLoading) return <Spin description="Loading playtest..." />
  if (error) return <Alert type="error" message="Failed to load playtest." />
  if (!playtest) return <Alert type="warning" message="Playtest not found." />

  return (
    <>
      <Typography.Title level={2}>Edit playtest</Typography.Title>
      <Typography.Text type="secondary">
        Slug <code>{playtest.slug}</code> · distribution model <code>{playtest.distributionModel}</code> (immutable after creation).
      </Typography.Text>
      <Form<FormValues> form={form} layout="vertical" onFinish={handleSubmit} style={{ marginTop: 16 }} initialValues={initialValues as FormValues}>
        <Form.Item label="Title" name="title" rules={[{ required: true, message: 'Title is required' }]}>
          <Input maxLength={200} />
        </Form.Item>
        <Form.Item label="Description" name="description">
          <Input.TextArea rows={4} maxLength={10000} />
        </Form.Item>
        <Form.Item label="Banner image URL" name="bannerImageUrl" rules={[{ type: 'url', message: 'Must be a URL' }]}>
          <Input placeholder="https://..." />
        </Form.Item>
        <Form.Item label="Platforms" name="platforms" rules={[{ required: true, message: 'Pick at least one' }]}>
          <Select mode="multiple" options={PLATFORMS.map(p => ({ value: p.value, label: p.label }))} />
        </Form.Item>
        <Form.Item label="Starts / Ends (display-only in MVP)" name="dateRange">
          <DatePicker.RangePicker showTime style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="ndaRequired" valuePropName="checked">
          <Checkbox>Require NDA</Checkbox>
        </Form.Item>
        <Form.Item
          noStyle
          shouldUpdate={(prev: FormValues, next: FormValues) => prev.ndaRequired !== next.ndaRequired}>
          {({ getFieldValue }) =>
            getFieldValue('ndaRequired') && (
              <Form.Item label="NDA text" name="ndaText" rules={[{ required: true, message: 'NDA text required when NDA enabled' }]}>
                <Input.TextArea rows={4} />
              </Form.Item>
            )
          }
        </Form.Item>
        {editMutation.isError && (
          <Form.Item>
            <Alert type="error" message={editMutation.error?.response?.data?.errorMessage ?? 'Update failed'} />
          </Form.Item>
        )}
        <Form.Item style={{ marginBottom: 0 }}>
          <div style={{ display: 'flex', gap: 8 }}>
            <Button onClick={() => navigate('/')}>Cancel</Button>
            <Button type="primary" htmlType="submit" loading={editMutation.isPending}>
              Save
            </Button>
          </div>
        </Form.Item>
      </Form>
    </>
  )
}

const APPLICANT_STATUS_TAG: Record<string, { text: string; color: string }> = {
  APPLICANT_STATUS_PENDING: { text: 'Pending', color: 'gold' },
  APPLICANT_STATUS_APPROVED: { text: 'Approved', color: 'green' },
  APPLICANT_STATUS_REJECTED: { text: 'Rejected', color: 'red' }
}

const DM_STATUS_TAG: Record<string, { text: string; color: string }> = {
  DM_STATUS_PENDING: { text: 'Pending', color: 'default' },
  DM_STATUS_SENT: { text: 'Sent', color: 'green' },
  DM_STATUS_FAILED: { text: 'Failed', color: 'red' }
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

function ApplicantsPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { playtestId = '' } = useParams()

  const [statusFilter, setStatusFilter] = useState<string>('APPLICANT_STATUS_UNSPECIFIED')
  const [dmFailedOnly, setDmFailedOnly] = useState(false)
  const [rejectTarget, setRejectTarget] = useState<V1Applicant | null>(null)
  const [rejectReason, setRejectReason] = useState('')

  const playtestQuery = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )

  const applicantsQuery = usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId(
    sdk,
    {
      playtestId,
      queryParams: {
        statusFilter: statusFilter as
          | 'APPLICANT_STATUS_UNSPECIFIED'
          | 'APPLICANT_STATUS_PENDING'
          | 'APPLICANT_STATUS_APPROVED'
          | 'APPLICANT_STATUS_REJECTED',
        dmFailedFilter: dmFailedOnly
      }
    },
    { enabled: !!playtestId }
  )

  const codesQuery = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )

  const invalidateApplicants = () => {
    queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Applicants_ByPlaytestId] })
    queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId] })
  }

  const approveMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation(sdk, {
    onSuccess: () => {
      message.success('Applicant approved')
      invalidateApplicants()
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to approve')
  })

  const rejectMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation(sdk, {
    onSuccess: () => {
      message.success('Applicant rejected')
      setRejectTarget(null)
      setRejectReason('')
      invalidateApplicants()
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to reject')
  })

  const retryDmMutation = usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation(sdk, {
    onSuccess: () => {
      message.success('Retry DM enqueued')
      invalidateApplicants()
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to retry DM')
  })

  const playtest = playtestQuery.data?.playtest as V1Playtest | undefined
  const applicants = (applicantsQuery.data?.applicants ?? []) as V1Applicant[]
  const stats = codesQuery.data?.stats

  const columns = [
    { title: 'Discord', dataIndex: 'discordHandle', key: 'discordHandle', render: (v: string | null | undefined) => v ?? '—' },
    {
      title: 'Platforms',
      dataIndex: 'platforms',
      key: 'platforms',
      render: (v: string[] | null | undefined) => (v ?? []).map(p => p.replace('PLATFORM_', '').toLowerCase()).join(', ') || '—'
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (v: string | null | undefined) => {
        const info = APPLICANT_STATUS_TAG[v ?? ''] ?? { text: v ?? '—', color: 'default' }
        return <Tag color={info.color}>{info.text}</Tag>
      }
    },
    {
      title: 'DM',
      dataIndex: 'lastDmStatus',
      key: 'lastDmStatus',
      render: (v: string | null | undefined, row: V1Applicant) => {
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
      title: 'Created',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—')
    },
    {
      title: 'Actions',
      key: 'actions',
      render: (_: unknown, row: V1Applicant) => {
        const isPending = row.status === 'APPLICANT_STATUS_PENDING'
        const canRetryDm = row.status === 'APPLICANT_STATUS_APPROVED' && row.lastDmStatus === 'DM_STATUS_FAILED'
        return (
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <Popconfirm
              title="Approve this applicant?"
              description="A code will be reserved and granted from the pool."
              okText="Approve"
              disabled={!isPending}
              onConfirm={() => approveMutation.mutate({ applicantId: row.id ?? '', data: {} })}>
              <Button size="small" type="primary" disabled={!isPending}>
                Approve
              </Button>
            </Popconfirm>
            <Button size="small" danger disabled={!isPending} onClick={() => setRejectTarget(row)}>
              Reject
            </Button>
            {canRetryDm && (
              <Button size="small" onClick={() => retryDmMutation.mutate({ applicantId: row.id ?? '', data: {} })}>
                Retry DM
              </Button>
            )}
          </div>
        )
      }
    }
  ]

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            Applicants
          </Typography.Title>
          <Typography.Text type="secondary">
            {playtest?.title ? `${playtest.title} (${playtest.slug})` : 'Loading playtest…'}
          </Typography.Text>
        </div>
        <Space>
          <Button onClick={() => navigate('/')}>Back</Button>
          <Button onClick={() => applicantsQuery.refetch()}>Refresh</Button>
        </Space>
      </div>

      <LowPoolBanner stats={stats} />

      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          aria-label="Status filter"
          value={statusFilter}
          onChange={setStatusFilter}
          style={{ width: 200 }}
          options={[
            { value: 'APPLICANT_STATUS_UNSPECIFIED', label: 'All statuses' },
            { value: 'APPLICANT_STATUS_PENDING', label: 'Pending' },
            { value: 'APPLICANT_STATUS_APPROVED', label: 'Approved' },
            { value: 'APPLICANT_STATUS_REJECTED', label: 'Rejected' }
          ]}
        />
        <Checkbox checked={dmFailedOnly} onChange={e => setDmFailedOnly(e.target.checked)}>
          DM failed only
        </Checkbox>
      </Space>

      {applicantsQuery.isLoading && <Spin description="Loading applicants..." />}
      {applicantsQuery.error && (
        <Alert
          type="error"
          message="Failed to load applicants."
          action={
            <Button size="small" onClick={() => applicantsQuery.refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!applicantsQuery.isLoading && !applicantsQuery.error && (
        <Table<V1Applicant>
          rowKey={row => row.id ?? ''}
          dataSource={applicants}
          columns={columns}
          pagination={{ pageSize: 50 }}
        />
      )}

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
            applicantId: rejectTarget.id ?? '',
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
    </>
  )
}

const CODE_STATE_TAG: Record<string, { text: string; color: string }> = {
  CODE_STATE_UNUSED: { text: 'Unused', color: 'default' },
  CODE_STATE_RESERVED: { text: 'Reserved', color: 'gold' },
  CODE_STATE_GRANTED: { text: 'Granted', color: 'green' }
}

function CodePoolPage() {
  const { sdk } = useAppUIContext()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { playtestId = '' } = useParams()

  const [csvText, setCsvText] = useState('')
  const [csvFilename, setCsvFilename] = useState('')
  const [topUpQty, setTopUpQty] = useState<number | null>(100)
  const [rejections, setRejections] = useState<V1UploadCodesRejection[]>([])

  const playtestQuery = usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: !!playtestId }
  )
  const codesQuery = usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId(sdk, { playtestId }, { enabled: !!playtestId })

  const invalidateCodes = () => queryClient.invalidateQueries({ queryKey: [Key_PlaytesthubServiceAdmin.Codes_ByPlaytestId] })

  const uploadMutation = usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation(sdk, {
    onSuccess: response => {
      const r = (response.rejections ?? []) as V1UploadCodesRejection[]
      setRejections(r)
      if (r.length === 0) {
        message.success(`Inserted ${response.inserted ?? 0} codes`)
        setCsvText('')
        setCsvFilename('')
      } else {
        message.warning(`Upload rejected: ${r.length} invalid line${r.length === 1 ? '' : 's'}`)
      }
      invalidateCodes()
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to upload codes')
  })

  const topUpMutation = usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation(sdk, {
    onSuccess: response => {
      message.success(`Generated ${response.added ?? 0} new codes`)
      invalidateCodes()
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to top up')
  })

  const syncMutation = usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation(sdk, {
    onSuccess: response => {
      message.success(`Synced ${response.added ?? 0} new codes from AGS`)
      invalidateCodes()
    },
    onError: err => message.error(err.response?.data?.errorMessage ?? 'Failed to sync from AGS')
  })

  const playtest = playtestQuery.data?.playtest as V1Playtest | undefined
  const stats = codesQuery.data?.stats
  const codes = (codesQuery.data?.codes ?? []) as V1Code[]
  const isAGS = playtest?.distributionModel === 'DISTRIBUTION_MODEL_AGS_CAMPAIGN'

  const handleFileChosen = (file: UploadFile) => {
    const blob = file.originFileObj as Blob | undefined
    if (!blob) return false
    const reader = new FileReader()
    reader.onload = () => {
      setCsvText(typeof reader.result === 'string' ? reader.result : '')
      setCsvFilename(file.name ?? '')
      setRejections([])
    }
    reader.readAsText(blob)
    return false
  }

  const codeColumns = [
    { title: 'Value', dataIndex: 'value', key: 'value', render: (v: string | null | undefined) => v ?? '—' },
    {
      title: 'State',
      dataIndex: 'state',
      key: 'state',
      render: (v: string | null | undefined) => {
        const info = CODE_STATE_TAG[v ?? ''] ?? { text: v ?? '—', color: 'default' }
        return <Tag color={info.color}>{info.text}</Tag>
      }
    },
    {
      title: 'Created',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—')
    }
  ]

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <div>
          <Typography.Title level={2} style={{ margin: 0 }}>
            Code pool
          </Typography.Title>
          <Typography.Text type="secondary">
            {playtest?.title ? `${playtest.title} (${playtest.slug})` : 'Loading playtest…'}
            {playtest && (
              <>
                {' · '}
                <code>{playtest.distributionModel}</code>
              </>
            )}
          </Typography.Text>
        </div>
        <Space>
          <Button onClick={() => navigate('/')}>Back</Button>
          <Button onClick={() => codesQuery.refetch()}>Refresh</Button>
        </Space>
      </div>

      <LowPoolBanner stats={stats} />

      <div style={{ display: 'flex', gap: 24, marginBottom: 24, flexWrap: 'wrap' }}>
        <Statistic title="Total" value={stats?.total ?? 0} />
        <Statistic title="Unused" value={stats?.unused ?? 0} />
        <Statistic title="Reserved" value={stats?.reserved ?? 0} />
        <Statistic title="Granted" value={stats?.granted ?? 0} />
      </div>

      {!isAGS && (
        <div style={{ marginBottom: 24 }}>
          <Typography.Title level={4}>Upload Steam keys</Typography.Title>
          <Typography.Paragraph type="secondary">
            One code per line. UTF-8, max 10 MB, max 50,000 lines, charset <code>[A-Za-z0-9._-]</code>, length 1–128. Any
            invalid line rejects the whole file.
          </Typography.Paragraph>
          <Upload accept=".csv,.txt,text/plain,text/csv" beforeUpload={() => false} onChange={info => handleFileChosen(info.file)} maxCount={1} showUploadList={false}>
            <Button>Choose file</Button>
          </Upload>
          {csvFilename && (
            <Typography.Paragraph style={{ marginTop: 8 }}>
              Selected: <code>{csvFilename}</code>
            </Typography.Paragraph>
          )}
          <Button
            type="primary"
            disabled={!csvText}
            loading={uploadMutation.isPending}
            style={{ marginTop: 8 }}
            onClick={() => uploadMutation.mutate({ playtestId, data: { csvContent: csvText, filename: csvFilename || undefined } })}>
            Upload
          </Button>
          {rejections.length > 0 && (
            <Alert
              type="error"
              style={{ marginTop: 12 }}
              message={`Upload rejected — ${rejections.length} invalid line${rejections.length === 1 ? '' : 's'}`}
              description={
                <ul style={{ margin: 0, paddingLeft: 20 }}>
                  {rejections.slice(0, 50).map((rej, i) => (
                    <li key={i}>
                      Line {rej.lineNumber}: {rej.reason}
                      {rej.value ? ` — ${rej.value}` : ''}
                    </li>
                  ))}
                  {rejections.length > 50 && <li>…and {rejections.length - 50} more.</li>}
                </ul>
              }
            />
          )}
        </div>
      )}

      {isAGS && (
        <div style={{ marginBottom: 24 }}>
          <Typography.Title level={4}>Generate / sync AGS Campaign codes</Typography.Title>
          <Typography.Paragraph type="secondary">
            Top-up calls AGS to generate fresh codes. Sync re-fetches from AGS to recover from a previous failure (idempotent).
          </Typography.Paragraph>
          <Space>
            <InputNumber min={1} max={50000} value={topUpQty} onChange={v => setTopUpQty(typeof v === 'number' ? v : null)} />
            <Button
              type="primary"
              disabled={!topUpQty || topUpQty < 1}
              loading={topUpMutation.isPending}
              onClick={() => topUpQty && topUpMutation.mutate({ playtestId, data: { quantity: topUpQty } })}>
              Generate more codes
            </Button>
            <Button loading={syncMutation.isPending} onClick={() => syncMutation.mutate({ playtestId, data: {} })}>
              Sync from AGS
            </Button>
          </Space>
        </div>
      )}

      <Typography.Title level={4}>Codes</Typography.Title>
      {codesQuery.isLoading && <Spin description="Loading codes..." />}
      {codesQuery.error && (
        <Alert
          type="error"
          message="Failed to load codes."
          action={
            <Button size="small" onClick={() => codesQuery.refetch()}>
              Retry
            </Button>
          }
        />
      )}
      {!codesQuery.isLoading && !codesQuery.error && (
        <Table<V1Code> rowKey={row => row.id ?? ''} dataSource={codes} columns={codeColumns} pagination={{ pageSize: 50 }} />
      )}
    </>
  )
}
