import { useAppUIContext } from '@accelbyte/sdk-extend-app-ui'
import { Alert, Select, Space, Table, Typography } from 'antd'
import dayjs from 'dayjs'
import { useMemo, useState } from 'react'
import type { V1Playtest } from '../playtesthubapi/generated-definitions/V1Playtest'
import type { V1Survey } from '../playtesthubapi/generated-definitions/V1Survey'
import type { V1SurveyAnswer } from '../playtesthubapi/generated-definitions/V1SurveyAnswer'
import type { V1SurveyResponse } from '../playtesthubapi/generated-definitions/V1SurveyResponse'
import { usePlaytesthubServiceAdminApi_GetSurveyResponses_ByPlaytestId } from '../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query'
import { usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId } from '../playtesthubapi/generated-public/queries/PlaytesthubService.query'

const QUESTION_TYPE_TEXT = 'SURVEY_QUESTION_TYPE_TEXT'
const QUESTION_TYPE_RATING = 'SURVEY_QUESTION_TYPE_RATING'
const QUESTION_TYPE_MULTI_CHOICE = 'SURVEY_QUESTION_TYPE_MULTI_CHOICE'

type AnswerAggregate = {
  questionId: string
  prompt: string
  type: string
  textCount: number
  ratingCounts: Record<number, number>
  optionCounts: Record<string, number>
  optionLabels: Record<string, string>
}

function buildAggregate(survey: V1Survey | undefined, responses: V1SurveyResponse[]): AnswerAggregate[] {
  const questions = survey?.questions ?? []
  return questions.map(q => {
    const agg: AnswerAggregate = {
      questionId: q.id ?? '',
      prompt: q.prompt ?? '',
      type: typeof q.type === 'string' ? q.type : '',
      textCount: 0,
      ratingCounts: {},
      optionCounts: {},
      optionLabels: Object.fromEntries((q.options ?? []).map(o => [o.id ?? '', o.label ?? '']))
    }
    for (const resp of responses) {
      const answers = (resp.answers ?? []) as V1SurveyAnswer[]
      const a = answers.find(x => x.questionId === q.id)
      if (!a) continue
      if (q.type === QUESTION_TYPE_TEXT && a.text) agg.textCount += 1
      if (q.type === QUESTION_TYPE_RATING && typeof a.rating === 'number') {
        agg.ratingCounts[a.rating] = (agg.ratingCounts[a.rating] ?? 0) + 1
      }
      if (q.type === QUESTION_TYPE_MULTI_CHOICE && a.multiChoice?.optionIds) {
        for (const id of a.multiChoice.optionIds) agg.optionCounts[id] = (agg.optionCounts[id] ?? 0) + 1
      }
    }
    return agg
  })
}

export function ResponsesTab({ playtest }: { playtest: V1Playtest }) {
  const { sdk } = useAppUIContext()
  const playtestId = playtest.id ?? ''
  const hasSurvey = Boolean(playtest.surveyId)

  const surveyQuery = usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId(
    sdk,
    { playtestId },
    { enabled: hasSurvey, retry: false }
  )
  const survey = surveyQuery.data?.survey as V1Survey | undefined

  const [surveyIdFilter, setSurveyIdFilter] = useState<string | undefined>(undefined)

  const responsesQuery = usePlaytesthubServiceAdminApi_GetSurveyResponses_ByPlaytestId(sdk, {
    playtestId,
    queryParams: { surveyIdFilter, pageSize: 200 }
  })
  const responses = useMemo(() => (responsesQuery.data?.responses ?? []) as V1SurveyResponse[], [responsesQuery.data])

  const versions = useMemo(() => {
    const seen = new Set<string>()
    for (const r of responses) {
      if (r.surveyId) seen.add(r.surveyId)
    }
    if (survey?.id) seen.add(survey.id)
    return Array.from(seen)
  }, [responses, survey])

  const grouped = useMemo(() => {
    const m = new Map<string, V1SurveyResponse[]>()
    for (const r of responses) {
      const key = r.surveyId ?? 'unknown'
      const arr = m.get(key) ?? []
      arr.push(r)
      m.set(key, arr)
    }
    return m
  }, [responses])

  const aggregate = useMemo(() => buildAggregate(survey, responses), [survey, responses])

  const responseColumns = [
    {
      title: 'Submitted',
      dataIndex: 'submittedAt',
      key: 'submittedAt',
      render: (v: string | null | undefined) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm') : '—')
    },
    { title: 'User', dataIndex: 'userId', key: 'userId' },
    {
      title: 'Answers',
      key: 'answers',
      render: (_: unknown, row: V1SurveyResponse) => {
        const answers = (row.answers ?? []) as V1SurveyAnswer[]
        return (
          <Space direction="vertical" size={2}>
            {answers.map((a, i) => {
              if (a.text) return <Typography.Text key={i}>“{a.text}”</Typography.Text>
              if (typeof a.rating === 'number') return <Typography.Text key={i}>★ {a.rating}/5</Typography.Text>
              if (a.multiChoice?.optionIds?.length) return <Typography.Text key={i}>✓ {a.multiChoice.optionIds.length} option(s)</Typography.Text>
              return null
            })}
          </Space>
        )
      }
    }
  ]

  return (
    <Space direction="vertical" style={{ width: '100%' }} data-testid="responses-tab">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <Typography.Text type="secondary">{responses.length} response(s) total · {versions.length} version(s)</Typography.Text>
        <Select
          allowClear
          placeholder="All versions"
          style={{ minWidth: 240 }}
          value={surveyIdFilter}
          onChange={val => setSurveyIdFilter(val ?? undefined)}
          options={versions.map(v => ({ value: v, label: v === survey?.id ? `${v} (current)` : v }))}
        />
      </div>

      {!hasSurvey && <Alert type="info" showIcon message="No survey configured for this playtest." />}

      {hasSurvey && (
        <>
          <Typography.Title level={4}>Aggregates</Typography.Title>
          {aggregate.length === 0 && <Typography.Text type="secondary">No survey questions to aggregate.</Typography.Text>}
          <Space direction="vertical" size="middle" style={{ display: 'flex', marginBottom: 24 }}>
            {aggregate.map(a => (
              <div key={a.questionId} data-testid="survey-aggregate" style={{ border: '1px solid #f0f0f0', borderRadius: 6, padding: 12 }}>
                <Typography.Text strong>{a.prompt}</Typography.Text>
                <div style={{ marginTop: 8 }}>
                  {a.type === QUESTION_TYPE_TEXT && <Typography.Text>{a.textCount} text answer(s) — see rows below for content</Typography.Text>}
                  {a.type === QUESTION_TYPE_RATING && (
                    <Space direction="vertical" size={2} style={{ display: 'flex' }}>
                      {[1, 2, 3, 4, 5].map(n => (
                        <div key={n} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ width: 32 }}>★ {n}</span>
                          <span data-testid={`rating-bar-${a.questionId}-${n}`} style={{ flex: 1 }}>
                            <span
                              style={{
                                display: 'inline-block',
                                height: 8,
                                background: '#1677ff',
                                width: `${Math.min(100, (a.ratingCounts[n] ?? 0) * 20)}%`
                              }}
                            />
                          </span>
                          <span style={{ width: 32, textAlign: 'right' }}>{a.ratingCounts[n] ?? 0}</span>
                        </div>
                      ))}
                    </Space>
                  )}
                  {a.type === QUESTION_TYPE_MULTI_CHOICE && (
                    <Space direction="vertical" size={2} style={{ display: 'flex' }}>
                      {Object.entries(a.optionLabels).map(([id, label]) => (
                        <div key={id} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ width: 200 }}>{label}</span>
                          <span data-testid={`option-bar-${a.questionId}-${id}`} style={{ flex: 1 }}>
                            <span
                              style={{
                                display: 'inline-block',
                                height: 8,
                                background: '#1677ff',
                                width: `${Math.min(100, (a.optionCounts[id] ?? 0) * 10)}%`
                              }}
                            />
                          </span>
                          <span style={{ width: 32, textAlign: 'right' }}>{a.optionCounts[id] ?? 0}</span>
                        </div>
                      ))}
                    </Space>
                  )}
                </div>
              </div>
            ))}
          </Space>

          <Typography.Title level={4}>Responses</Typography.Title>
          {Array.from(grouped.entries()).map(([surveyVersionId, rows]) => (
            <div key={surveyVersionId} style={{ marginBottom: 24 }}>
              <Typography.Text strong>
                Survey {surveyVersionId === survey?.id ? `${surveyVersionId} (current)` : surveyVersionId}
              </Typography.Text>
              <Table<V1SurveyResponse>
                rowKey={row => row.id ?? ''}
                dataSource={rows}
                columns={responseColumns}
                pagination={{ pageSize: 50 }}
                size="small"
                style={{ marginTop: 8 }}
              />
            </div>
          ))}
        </>
      )}
    </Space>
  )
}
