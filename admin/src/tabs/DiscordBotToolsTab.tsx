/**
 * DiscordBotToolsTab — M5.C phase 7 / docs/STATUS_M5.md D8 + C7.
 *
 * Two side-by-side cards: Create Announcement form (subject + message
 * + Send-To filter + char counters) and the Announcement History table
 * (status pill aggregated server-side from announcement_recipient).
 */

import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Form, Input, Select, Space, Table, Tag, Typography, message } from 'antd'
import dayjs from 'dayjs'
import { useMemo } from 'react'
import type { V1Announcement } from '../playtesthubapi/generated-definitions/V1Announcement'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'
import {
  Key_PlaytesthubServiceAdmin,
  usePlaytesthubServiceAdminApi_CreateAnnouncement_ByPlaytestIdMutation,
  usePlaytesthubServiceAdminApi_GetAnnouncements_ByPlaytestId
} from '../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'

const PlaytestStatus = {
  CLOSED: 'PLAYTEST_STATUS_CLOSED'
} as const

const AnnouncementSendToFilter = {
  ALL: 'ANNOUNCEMENT_SEND_TO_FILTER_ALL',
  APPROVED_ONLY: 'ANNOUNCEMENT_SEND_TO_FILTER_APPROVED_ONLY',
  PENDING_ONLY: 'ANNOUNCEMENT_SEND_TO_FILTER_PENDING_ONLY'
} as const

const STATUS_TAG: Record<string, { text: string; color: string }> = {
  ANNOUNCEMENT_STATUS_SENT: { text: 'Sent', color: 'green' },
  ANNOUNCEMENT_STATUS_SENDING: { text: 'Sending', color: 'blue' },
  ANNOUNCEMENT_STATUS_PARTIAL: { text: 'Partial', color: 'orange' },
  ANNOUNCEMENT_STATUS_FAILED: { text: 'Failed', color: 'red' }
}

const SUBJECT_MAX = 200
const MESSAGE_MAX = 4000

type FormValues = {
  sendToFilter: string
  subject: string
  message: string
}

export function DiscordBotToolsTab({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const queryClient = useQueryClient()
  const [form] = Form.useForm<FormValues>()

  const closed = playtest.status === PlaytestStatus.CLOSED

  const history = usePlaytesthubServiceAdminApi_GetAnnouncements_ByPlaytestId(sdk, {
    playtestId: playtest.id ?? ''
  })

  const createMutation = usePlaytesthubServiceAdminApi_CreateAnnouncement_ByPlaytestIdMutation(sdk, {
    onSuccess: () => {
      message.success('Announcement queued')
      form.resetFields()
      queryClient.invalidateQueries({
        queryKey: [Key_PlaytesthubServiceAdmin.Announcements_ByPlaytestId]
      })
    },
    onError: (err: { response?: { data?: { errorMessage?: string } } }) =>
      message.error(err?.response?.data?.errorMessage ?? 'Failed to send announcement')
  })

  const announcements = useMemo(
    () => (history.data?.announcements ?? []) as V1Announcement[],
    [history.data]
  )

  const onFinish = (values: FormValues) => {
    createMutation.mutate({
      playtestId: playtest.id ?? '',
      data: {
        sendToFilter: values.sendToFilter as keyof typeof AnnouncementSendToFilter extends never
          ? never
          : (typeof AnnouncementSendToFilter)[keyof typeof AnnouncementSendToFilter],
        subject: values.subject,
        message: values.message
      } as never
    })
  }

  const columns = [
    {
      title: 'Date',
      dataIndex: 'createdAt',
      key: 'createdAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—')
    },
    { title: 'Subject', dataIndex: 'subject', key: 'subject' },
    {
      title: 'Recipients',
      key: 'recipients',
      render: (_: unknown, row: V1Announcement) =>
        `${row.recipientsSent ?? 0} / ${row.recipientsTotal ?? 0}`
    },
    {
      title: 'Status',
      dataIndex: 'status',
      key: 'status',
      render: (v: string) => {
        const tag = STATUS_TAG[v] ?? { text: v ?? '—', color: 'default' }
        return <Tag color={tag.color}>{tag.text}</Tag>
      }
    }
  ]

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }} data-testid="bot-tools-tab">
      <Card title="Create announcement">
        {closed && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="Announcements cannot be sent for closed playtests."
            data-testid="bot-tools-closed-banner"
          />
        )}
        <Form<FormValues>
          form={form}
          layout="vertical"
          initialValues={{ sendToFilter: AnnouncementSendToFilter.APPROVED_ONLY }}
          onFinish={onFinish}
          disabled={closed}>
          <Form.Item label="Send to" name="sendToFilter" rules={[{ required: true }]}>
            <Select
              data-testid="bot-tools-send-to"
              options={[
                { value: AnnouncementSendToFilter.ALL, label: 'All applicants' },
                { value: AnnouncementSendToFilter.APPROVED_ONLY, label: 'Approved only' },
                { value: AnnouncementSendToFilter.PENDING_ONLY, label: 'Pending only' }
              ]}
            />
          </Form.Item>
          <Form.Item
            label="Subject"
            name="subject"
            rules={[
              { required: true, message: 'announcement subject must not be empty' },
              { max: SUBJECT_MAX, message: `announcement subject must be at most ${SUBJECT_MAX} characters` }
            ]}>
            <Input maxLength={SUBJECT_MAX} placeholder="e.g. Playtest build updated — v2.1 now available" data-testid="bot-tools-subject" />
          </Form.Item>
          <Form.Item
            label="Message"
            name="message"
            rules={[
              { required: true, message: 'announcement message must not be empty' },
              { max: MESSAGE_MAX, message: `announcement message must be at most ${MESSAGE_MAX} characters` }
            ]}>
            <Input.TextArea
              autoSize={{ minRows: 4, maxRows: 12 }}
              maxLength={MESSAGE_MAX}
              showCount
              placeholder="Write the message that will be sent to players via Discord DM..."
              data-testid="bot-tools-message"
            />
          </Form.Item>
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message="Messages will be sent via the PlaytestHub Discord Bot as direct messages. Delivery may take a few minutes depending on Discord rate limits."
          />
          <Space>
            <Button type="primary" htmlType="submit" loading={createMutation.isPending} data-testid="bot-tools-submit">
              Send via DM
            </Button>
          </Space>
        </Form>
      </Card>
      <Card title="Announcement history">
        {announcements.length === 0 && !history.isLoading ? (
          <Typography.Text type="secondary">
            No announcements sent yet — your first broadcast appears here.
          </Typography.Text>
        ) : (
          <Table<V1Announcement>
            rowKey={row => row.id ?? ''}
            loading={history.isLoading}
            dataSource={announcements}
            columns={columns}
            pagination={{ pageSize: 10 }}
          />
        )}
      </Card>
    </div>
  )
}
