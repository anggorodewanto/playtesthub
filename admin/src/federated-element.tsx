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
  Popconfirm,
  Radio,
  Select,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
  message
} from 'antd'
import dayjs, { type Dayjs } from 'dayjs'
import { useEffect, useMemo } from 'react'
import { Route, Routes, useNavigate, useParams } from 'react-router'
import type { V1Playtest } from './playtesthubapi/generated-definitions/V1Playtest'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreatePlaytestMutation,
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation,
  usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation,
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
          <div style={{ display: 'flex', gap: 8 }}>
            <Button size="small" onClick={() => navigate(`${row.id}/edit`)}>
              Edit
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
            <Tooltip title="AGS Campaign ships in M2 (PRD §10).">
              <Radio value="DISTRIBUTION_MODEL_AGS_CAMPAIGN" disabled>
                AGS Campaign <Tag>M2</Tag>
              </Radio>
            </Tooltip>
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

