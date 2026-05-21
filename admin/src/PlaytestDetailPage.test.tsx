import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@accelbyte/sdk-extend-app-ui', () => ({
  useAppUIContext: () => ({ sdk: {}, isCurrentUserHasPermission: () => true }),
  CrudType: { READ: 'READ', CREATE: 'CREATE', UPDATE: 'UPDATE', DELETE: 'DELETE' }
}))

const mockGetPlaytests = vi.fn()
const mockTransition = vi.fn()
const mockGetParticipants = vi.fn()
const mockGetApplicants = vi.fn()
const mockGetAnnouncements = vi.fn()
const mockCreateAnnouncement = vi.fn()
const mockGetCodes = vi.fn()
const mockUploadCodes = vi.fn()
const mockTopUpCodes = vi.fn()
const mockSyncCodes = vi.fn()
const mockGetAdtLinkages = vi.fn()
const mockApprove = vi.fn()
const mockReject = vi.fn()
const mockRetryDm = vi.fn()
const mockGetPublicConfig = vi.fn()
const mockCreateSurvey = vi.fn()
const mockPatchSurvey = vi.fn()
const mockGetSurveyResponses = vi.fn()
const mockGetSurveyPlayer = vi.fn()
const mockGetAuditLog = vi.fn()

vi.mock('./playtesthubapi/generated-public/queries/PlaytesthubService.query', () => ({
  usePlaytesthubServiceApi_GetConfig: (...a: unknown[]) => mockGetPublicConfig(...a),
  usePlaytesthubServiceApi_GetSurveyPlayer_ByPlaytestId: (...a: unknown[]) => mockGetSurveyPlayer(...a)
}))

vi.mock('./playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query', () => ({
  Key_PlaytesthubServiceAdmin: {
    Playtests: 'playtests',
    Playtest_ByPlaytestId: 'playtest-by-id',
    Participants_ByPlaytestId: 'participants',
    Applicants_ByPlaytestId: 'applicants',
    Announcements_ByPlaytestId: 'announcements',
    Codes_ByPlaytestId: 'codes-by-playtest-id',
    Survey_ByPlaytestId: 'survey-by-playtest-id'
  },
  usePlaytesthubServiceAdminApi_GetPlaytests: (...a: unknown[]) => mockGetPlaytests(...a),
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation: (...a: unknown[]) =>
    mockTransition(...a),
  usePlaytesthubServiceAdminApi_GetParticipants_ByPlaytestId: (...a: unknown[]) => mockGetParticipants(...a),
  usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId: (...a: unknown[]) => mockGetApplicants(...a),
  usePlaytesthubServiceAdminApi_GetAnnouncements_ByPlaytestId: (...a: unknown[]) => mockGetAnnouncements(...a),
  usePlaytesthubServiceAdminApi_CreateAnnouncement_ByPlaytestIdMutation: (...a: unknown[]) =>
    mockCreateAnnouncement(...a),
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId: (...a: unknown[]) => mockGetCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation: (...a: unknown[]) => mockUploadCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation: (...a: unknown[]) => mockTopUpCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation: (...a: unknown[]) => mockSyncCodes(...a),
  usePlaytesthubServiceAdminApi_GetAdtLinkages: (...a: unknown[]) => mockGetAdtLinkages(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation: (...a: unknown[]) =>
    mockApprove(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation: (...a: unknown[]) => mockReject(...a),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation: (...a: unknown[]) => mockRetryDm(...a),
  usePlaytesthubServiceAdminApi_CreateSurvey_ByPlaytestIdMutation: (...a: unknown[]) => mockCreateSurvey(...a),
  usePlaytesthubServiceAdminApi_PatchSurvey_ByPlaytestIdMutation: (...a: unknown[]) => mockPatchSurvey(...a),
  usePlaytesthubServiceAdminApi_GetSurveyResponses_ByPlaytestId: (...a: unknown[]) => mockGetSurveyResponses(...a),
  usePlaytesthubServiceAdminApi_GetAuditLog_ByPlaytestId: (...a: unknown[]) => mockGetAuditLog(...a)
}))

import { PlaytestDetailPage } from './PlaytestDetailPage'

const DRAFT_PT = {
  id: 'pt-draft',
  namespace: 'ns',
  slug: 'autumn-draft',
  title: 'Autumn Build',
  description: 'an autumn playtest',
  bannerImageUrl: 'https://example.com/banner.png',
  platforms: ['PLATFORM_STEAM'],
  status: 'PLAYTEST_STATUS_DRAFT',
  distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
  ndaRequired: false,
  autoApprove: false
}

const OPEN_PT = { ...DRAFT_PT, id: 'pt-open', slug: 'autumn-open', status: 'PLAYTEST_STATUS_OPEN' }
const CLOSED_PT = { ...DRAFT_PT, id: 'pt-closed', slug: 'autumn-closed', status: 'PLAYTEST_STATUS_CLOSED' }

function renderDetail(slug: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/playtest/${slug}`]}>
        <Routes>
          <Route path="/playtest/:slug" element={<PlaytestDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockGetPlaytests.mockReturnValue({
    data: { playtests: [DRAFT_PT, OPEN_PT, CLOSED_PT] },
    isLoading: false,
    error: undefined,
    refetch: vi.fn()
  })
  mockTransition.mockReturnValue({ mutate: vi.fn() })
  mockGetParticipants.mockReturnValue({ data: { participants: [] }, isLoading: false, refetch: vi.fn() })
  mockGetApplicants.mockReturnValue({ data: { applicants: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockGetAnnouncements.mockReturnValue({ data: { announcements: [] }, isLoading: false, refetch: vi.fn() })
  mockCreateAnnouncement.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockGetCodes.mockReturnValue({ data: { stats: { total: 0, unused: 0, granted: 0 }, codes: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockUploadCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockTopUpCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockSyncCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockGetAdtLinkages.mockReturnValue({ data: { linkages: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockApprove.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockReject.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockRetryDm.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: 'https://play.example.com' } })
  mockCreateSurvey.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockPatchSurvey.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetSurveyResponses.mockReturnValue({ data: { responses: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockGetSurveyPlayer.mockReturnValue({ data: undefined, isLoading: false, isError: false, error: null })
  mockGetAuditLog.mockReturnValue({ data: { entries: [], nextPageToken: '' }, isLoading: false, error: null, refetch: vi.fn() })
})

describe('PlaytestDetailPage shell', () => {
  it('renders header + status pill + breadcrumb', () => {
    renderDetail('autumn-draft')
    expect(screen.getByRole('heading', { name: 'Autumn Build', level: 2 })).toBeInTheDocument()
    const pill = screen.getByTestId('playtest-status-pill')
    expect(within(pill).getByText('Draft')).toBeInTheDocument()
  })

  it('renders Publish in DRAFT only', () => {
    renderDetail('autumn-draft')
    expect(screen.getByTestId('header-publish')).toBeInTheDocument()
    expect(screen.queryByTestId('header-stop')).not.toBeInTheDocument()
  })

  it('renders Stop Playtest in OPEN only', () => {
    renderDetail('autumn-open')
    expect(screen.getByTestId('header-stop')).toBeInTheDocument()
    expect(screen.queryByTestId('header-publish')).not.toBeInTheDocument()
  })

  it('hides Publish and Stop in CLOSED', () => {
    renderDetail('autumn-closed')
    expect(screen.queryByTestId('header-publish')).not.toBeInTheDocument()
    expect(screen.queryByTestId('header-stop')).not.toBeInTheDocument()
  })

  it('falls back to Info tab when ?tab missing', () => {
    renderDetail('autumn-draft')
    expect(screen.getByTestId('playtest-info-tab')).toBeInTheDocument()
  })

  it('persists ?tab= changes across reloads via search params', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    await waitFor(() => expect(screen.getByTestId('distribution-tab')).toBeInTheDocument())
  })

  it('Info tab renders the read-only field grid + Edit button', () => {
    renderDetail('autumn-draft')
    expect(screen.getByTestId('playtest-info-tab')).toBeInTheDocument()
    expect(screen.getByText('an autumn playtest')).toBeInTheDocument()
    expect(screen.getByTestId('playtest-info-edit')).toBeInTheDocument()
  })

  it('surfaces a warning when the slug is unknown', () => {
    renderDetail('does-not-exist')
    expect(screen.getByText(/not found/i)).toBeInTheDocument()
  })

  it('Discord Bot Tools tab disables the form on closed playtest', async () => {
    renderDetail('autumn-closed')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Discord Bot Tools' }))
    expect(await screen.findByTestId('bot-tools-closed-banner')).toBeInTheDocument()
  })

  it('Discord Bot Tools tab submits CreateAnnouncement with form values', async () => {
    const mutate = vi.fn()
    mockCreateAnnouncement.mockReturnValue({ mutate, isPending: false })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Discord Bot Tools' }))
    const subj = await screen.findByTestId('bot-tools-subject')
    const msg = screen.getByTestId('bot-tools-message')
    await user.type(subj, 'Build update')
    await user.type(msg, 'New patch live')
    await user.click(screen.getByTestId('bot-tools-submit'))
    await waitFor(() => expect(mutate).toHaveBeenCalled())
    expect(mutate.mock.calls[0][0]).toMatchObject({
      playtestId: 'pt-draft',
      data: {
        sendToFilter: 'ANNOUNCEMENT_SEND_TO_FILTER_APPROVED_ONLY',
        subject: 'Build update',
        message: 'New patch live'
      }
    })
  })

  it('Discord Bot Tools tab renders the empty-history copy', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Discord Bot Tools' }))
    expect(await screen.findByText(/No announcements sent yet/)).toBeInTheDocument()
  })

  it('Participants tab renders rows, capacity counter, and joined platform column', async () => {
    mockGetParticipants.mockReturnValue({
      data: {
        participants: [
          {
            applicantId: 'a1',
            userId: 'u1',
            discordHandle: 'alice#1',
            signupAt: '2026-05-20T00:00:00Z',
            ndaAcceptedAt: '2026-05-20T00:00:00Z',
            codeSentAt: '2026-05-20T01:00:00Z',
            status: 'APPLICANT_STATUS_APPROVED'
          },
          {
            applicantId: 'a2',
            userId: 'u2',
            discordHandle: 'bob#2',
            signupAt: '2026-05-20T00:00:00Z',
            status: 'APPLICANT_STATUS_PENDING'
          }
        ]
      },
      isLoading: false,
      refetch: vi.fn()
    })
    mockGetApplicants.mockReturnValue({
      data: {
        applicants: [
          { id: 'a1', discordHandle: 'alice#1', platforms: ['PLATFORM_STEAM'], status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_SENT' },
          { id: 'a2', discordHandle: 'bob#2', platforms: ['PLATFORM_XBOX'], status: 'APPLICANT_STATUS_PENDING' }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    expect(await screen.findByText('alice#1')).toBeInTheDocument()
    expect(screen.getByText('bob#2')).toBeInTheDocument()
    expect(screen.getByText('2 / ∞ enrolled')).toBeInTheDocument()
    expect(screen.getByText('steam')).toBeInTheDocument()
    expect(screen.getByText('xbox')).toBeInTheDocument()
    expect(screen.getByText('Sent')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /^approve$/i })[0]).toBeInTheDocument()
  })

  it('Participants tab shows Retry DM only for APPROVED participants whose last DM failed', async () => {
    mockGetParticipants.mockReturnValue({
      data: {
        participants: [
          { applicantId: 'a1', discordHandle: 'a', status: 'APPLICANT_STATUS_APPROVED' },
          { applicantId: 'a2', discordHandle: 'b', status: 'APPLICANT_STATUS_APPROVED' },
          { applicantId: 'a3', discordHandle: 'c', status: 'APPLICANT_STATUS_PENDING' }
        ]
      },
      isLoading: false,
      refetch: vi.fn()
    })
    mockGetApplicants.mockReturnValue({
      data: {
        applicants: [
          { id: 'a1', discordHandle: 'a', status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_FAILED' },
          { id: 'a2', discordHandle: 'b', status: 'APPLICANT_STATUS_APPROVED', lastDmStatus: 'DM_STATUS_SENT' },
          { id: 'a3', discordHandle: 'c', status: 'APPLICANT_STATUS_PENDING' }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    await screen.findByText('a')
    expect(screen.getAllByRole('button', { name: /retry dm/i })).toHaveLength(1)
  })

  it('Participants tab renders the low-pool banner when unused/total ≤ 10%', async () => {
    mockGetCodes.mockReturnValue({
      data: { stats: { total: 100, unused: 5, reserved: 0, granted: 95 }, codes: [] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    expect(await screen.findByText(/code pool is low/i)).toBeInTheDocument()
  })

  it('Participants tab opens the reject modal and submits the typed reason', async () => {
    const mutate = vi.fn()
    mockReject.mockReturnValue({ mutate, isPending: false })
    mockGetParticipants.mockReturnValue({
      data: { participants: [{ applicantId: 'a1', discordHandle: 'tester', status: 'APPLICANT_STATUS_PENDING' }] },
      isLoading: false,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    await user.click(await screen.findByRole('button', { name: /^reject$/i }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByPlaceholderText(/stored on the applicant row/i), 'duplicate signup')
    await user.click(within(dialog).getByRole('button', { name: /^reject$/i }))
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({ applicantId: 'a1', data: { rejectionReason: 'duplicate signup' } })
    )
  })

  it('Participants tab approve flow confirms via Popconfirm before mutating', async () => {
    const mutate = vi.fn()
    mockApprove.mockReturnValue({ mutate, isPending: false })
    mockGetParticipants.mockReturnValue({
      data: { participants: [{ applicantId: 'a1', discordHandle: 'tester', status: 'APPLICANT_STATUS_PENDING' }] },
      isLoading: false,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Participants' }))
    await user.click(await screen.findByRole('button', { name: /^approve$/i }))
    const popup = await screen.findByRole('tooltip')
    await user.click(within(popup).getByRole('button', { name: /^approve$/i }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ applicantId: 'a1', data: {} }))
  })

  it('Distribution tab renders the inline Steam upload form + empty-state hint when pool is empty', async () => {
    mockGetCodes.mockReturnValue({
      data: { stats: { total: 0, unused: 0, granted: 0 }, codes: [] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    expect(await screen.findByText(/upload steam keys/i)).toBeInTheDocument()
    expect(screen.getByText('No codes uploaded yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^choose file$/i })).toBeInTheDocument()
  })

  it('Distribution tab reads + uploads a Steam keys CSV inline', async () => {
    const mutate = vi.fn()
    mockUploadCodes.mockReturnValue({ mutate, isPending: false })
    const { container } = renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    const csv = 'K7R2P-9M4XW-Q6V1B\nJ9L5T-B2N8R-M3K7P\n'
    const file = new File([csv], 'dummycodes.txt', { type: 'text/plain' })
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, file)
    await waitFor(() => expect(screen.getByText('dummycodes.txt')).toBeInTheDocument())
    const uploadBtn = screen.getByRole('button', { name: /^upload$/i })
    await waitFor(() => expect(uploadBtn).not.toBeDisabled())
    await user.click(uploadBtn)
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({
        playtestId: 'pt-draft',
        data: { csvContent: csv, filename: 'dummycodes.txt' }
      })
    )
  })

  it('Distribution tab triggers AGS top-up + sync mutations inline', async () => {
    const topUp = vi.fn()
    const sync = vi.fn()
    mockTopUpCodes.mockReturnValue({ mutate: topUp, isPending: false })
    mockSyncCodes.mockReturnValue({ mutate: sync, isPending: false })
    const agsPt = { ...DRAFT_PT, slug: 'autumn-ags', distributionModel: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN' }
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [agsPt] },
      isLoading: false,
      error: undefined,
      refetch: vi.fn()
    })
    renderDetail('autumn-ags')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    await user.click(await screen.findByRole('button', { name: /generate more codes/i }))
    await waitFor(() => expect(topUp).toHaveBeenCalledWith({ playtestId: 'pt-draft', data: { quantity: 100 } }))
    await user.click(screen.getByRole('button', { name: /sync from ags/i }))
    await waitFor(() => expect(sync).toHaveBeenCalledWith({ playtestId: 'pt-draft', data: {} }))
  })

  it('Distribution tab renders ADT empty-state when adt_namespace is missing', async () => {
    const adtDraft = { ...DRAFT_PT, slug: 'autumn-adt', distributionModel: 'DISTRIBUTION_MODEL_ADT', adtNamespace: undefined }
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [adtDraft] },
      isLoading: false,
      error: undefined,
      refetch: vi.fn()
    })
    renderDetail('autumn-adt')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Distribution' }))
    expect(await screen.findByText('ADT Namespace Not Linked')).toBeInTheDocument()
  })

  it('Copy share link uses the backend-supplied player_base_url, not the admin host', async () => {
    mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: 'https://play.example.com/' } })
    const user = userEvent.setup()
    renderDetail('autumn-draft')
    await user.click(screen.getByRole('button', { name: /Playtest Link/ }))
    await waitFor(async () => {
      const text = await navigator.clipboard.readText()
      expect(text).toBe('https://play.example.com/#/playtest/autumn-draft')
    })
  })

  it('Copy share link shows an error when player_base_url is unset (no clipboard write)', async () => {
    mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: '' } })
    const user = userEvent.setup()
    renderDetail('autumn-draft')
    await user.click(screen.getByRole('button', { name: /Playtest Link/ }))
    expect(await screen.findByText(/PLAYER_BASE_URL/)).toBeInTheDocument()
    expect(await navigator.clipboard.readText()).toBe('')
  })

  it('Publish click triggers the transition mutation via confirm modal', async () => {
    const mutate = vi.fn()
    mockTransition.mockReturnValue({ mutate })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByTestId('header-publish'))
    // Confirm modal renders an OK Publish button distinct from the header.
    const modalButtons = await screen.findAllByRole('button', { name: 'Publish' })
    await user.click(modalButtons[modalButtons.length - 1])
    await waitFor(() => expect(mutate).toHaveBeenCalled())
    expect(mutate.mock.calls[0][0]).toMatchObject({
      playtestId: 'pt-draft',
      data: { targetStatus: 'PLAYTEST_STATUS_OPEN' }
    })
  })

  it('Survey tab renders a blank text question when the playtest has no survey', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Survey' }))
    await waitFor(() => expect(screen.getAllByTestId('survey-question')).toHaveLength(1))
    expect(screen.getByRole('button', { name: /create survey/i })).toBeInTheDocument()
  })

  it('Survey tab submits CreateSurvey with the typed question on a fresh playtest', async () => {
    const mutate = vi.fn()
    mockCreateSurvey.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Survey' }))
    const prompts = await screen.findAllByPlaceholderText(/what did you think of the build/i)
    await user.type(prompts[0], 'Did you like it?')
    await user.click(screen.getByRole('button', { name: /create survey/i }))
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({
        playtestId: 'pt-draft',
        data: {
          questions: [{ type: 'SURVEY_QUESTION_TYPE_TEXT', prompt: 'Did you like it?', required: false }]
        }
      })
    )
  })

  it('Survey tab preloads existing survey questions and renders Save new version', async () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [{ ...DRAFT_PT, id: 'pt-open-svy', slug: 'autumn-open-svy', status: 'PLAYTEST_STATUS_OPEN', surveyId: 'sur_1' }]
      },
      isLoading: false,
      error: undefined,
      refetch: vi.fn()
    })
    mockGetSurveyPlayer.mockReturnValue({
      data: {
        survey: {
          id: 'sur_1',
          version: 2,
          questions: [{ id: 'q1', type: 'SURVEY_QUESTION_TYPE_TEXT', prompt: 'Tell us', required: true }]
        }
      },
      isLoading: false,
      isError: false,
      error: null
    })
    renderDetail('autumn-open-svy')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Survey' }))
    await waitFor(() => expect(screen.getAllByTestId('survey-question')).toHaveLength(1))
    expect(screen.getByDisplayValue('Tell us')).toBeInTheDocument()
    expect(screen.getByText(/current version v2 \(saving creates v3\)/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /save new version/i })).toBeInTheDocument()
  })

  it('Survey tab warns about DRAFT preload when GetSurvey errors and the playtest is DRAFT', async () => {
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ ...DRAFT_PT, surveyId: 'sur_1' }] },
      isLoading: false,
      error: undefined,
      refetch: vi.fn()
    })
    mockGetSurveyPlayer.mockReturnValue({ data: undefined, isLoading: false, isError: true, error: null })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Survey' }))
    expect(await screen.findByText(/draft playtest survey can't be previewed/i)).toBeInTheDocument()
  })

  it('Responses tab shows the empty-state info banner when the playtest has no survey', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Responses' }))
    expect(await screen.findByText(/no survey configured for this playtest/i)).toBeInTheDocument()
  })

  it('Responses tab groups responses by survey version and renders a histogram bucket per option id', async () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [{ ...DRAFT_PT, id: 'pt-open-svy', slug: 'autumn-open-svy', status: 'PLAYTEST_STATUS_OPEN', surveyId: 'sur_2' }]
      },
      isLoading: false,
      error: undefined,
      refetch: vi.fn()
    })
    mockGetSurveyPlayer.mockReturnValue({
      data: {
        survey: {
          id: 'sur_2',
          version: 2,
          questions: [
            {
              id: 'q1',
              type: 'SURVEY_QUESTION_TYPE_MULTI_CHOICE',
              prompt: 'Which platforms?',
              options: [
                { id: 'o1', label: 'Steam' },
                { id: 'o2', label: 'Xbox' }
              ]
            },
            { id: 'q2', type: 'SURVEY_QUESTION_TYPE_RATING', prompt: 'Rate it' }
          ]
        }
      },
      isLoading: false,
      isError: false,
      error: null
    })
    mockGetSurveyResponses.mockReturnValue({
      data: {
        responses: [
          {
            id: 'r1',
            surveyId: 'sur_2',
            userId: 'u1',
            submittedAt: '2026-05-01T10:00:00Z',
            answers: [
              { questionId: 'q1', multiChoice: { optionIds: ['o1'] } },
              { questionId: 'q2', rating: 5 }
            ]
          },
          {
            id: 'r2',
            surveyId: 'sur_1',
            userId: 'u2',
            submittedAt: '2026-04-30T10:00:00Z',
            answers: [{ questionId: 'q1', multiChoice: { optionIds: ['o2'] } }]
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-open-svy')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Responses' }))
    await waitFor(() => expect(screen.getAllByTestId('survey-aggregate')).toHaveLength(2))
    expect(screen.getByText(/2 response\(s\) total/)).toBeInTheDocument()
    expect(screen.getByText(/sur_2 \(current\)/)).toBeInTheDocument()
    expect(screen.getByText('sur_1', { exact: false })).toBeInTheDocument()
    expect(screen.getByTestId('option-bar-q1-o1')).toBeInTheDocument()
    expect(screen.getByTestId('option-bar-q1-o2')).toBeInTheDocument()
    expect(screen.getByTestId('rating-bar-q2-5')).toBeInTheDocument()
  })

  it('Audit tab renders the actor + action filters', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Audit' }))
    expect(await screen.findByLabelText(/actor filter/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/action filter/i)).toBeInTheDocument()
  })

  it('Audit tab renders system actor as a tag and admin actor as code', async () => {
    mockGetAuditLog.mockReturnValue({
      data: {
        entries: [
          {
            id: 'a_1',
            action: 'applicant.approve',
            actorUserId: 'user-uuid-1',
            createdAt: '2026-05-01T10:00:00Z',
            beforeJson: '{}',
            afterJson: '{"applicantId":"app_1"}'
          },
          {
            id: 'a_2',
            action: 'code.upload',
            actorUserId: null,
            createdAt: '2026-05-01T11:00:00Z',
            beforeJson: '{}',
            afterJson: '{"count":42}'
          }
        ],
        nextPageToken: ''
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Audit' }))
    expect(await screen.findByText('user-uuid-1')).toBeInTheDocument()
    expect(screen.getAllByText('system').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('applicant.approve')).toBeInTheDocument()
    expect(screen.getByText('code.upload')).toBeInTheDocument()
  })

  it('Audit tab passes actorFilter=system when System is chosen', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Audit' }))
    const actorSelect = await screen.findByLabelText(/actor filter/i)
    await user.click(actorSelect)
    await user.click(await screen.findByText('System'))
    await waitFor(() => {
      const lastCall = mockGetAuditLog.mock.calls.at(-1)
      expect(lastCall?.[1].queryParams.actorFilter).toBe('system')
    })
  })

  it('Audit tab only commits the typed actor user id on Enter (not on every keystroke)', async () => {
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Audit' }))
    const actorSelect = await screen.findByLabelText(/actor filter/i)
    await user.click(actorSelect)
    await user.click(await screen.findByText(/admin user/i))
    const input = await screen.findByLabelText(/actor user id/i)
    await user.type(input, 'abc-123')
    const callsMidType = mockGetAuditLog.mock.calls.filter(
      c => c[1]?.queryParams?.actorFilter && c[1].queryParams.actorFilter !== 'system'
    )
    expect(callsMidType).toHaveLength(0)
    await user.keyboard('{Enter}')
    await waitFor(() => {
      const lastCall = mockGetAuditLog.mock.calls.at(-1)
      expect(lastCall?.[1].queryParams.actorFilter).toBe('abc-123')
    })
  })

  it('Audit tab expanding a row renders the JSON before/after diff and tags changed keys', async () => {
    mockGetAuditLog.mockReturnValue({
      data: {
        entries: [
          {
            id: 'a_1',
            action: 'survey.edit',
            actorUserId: 'admin-1',
            createdAt: '2026-05-01T10:00:00Z',
            beforeJson: '{"surveyId":"sur_1","questions":1}',
            afterJson: '{"surveyId":"sur_2","questions":1}'
          }
        ],
        nextPageToken: ''
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Audit' }))
    await user.click(await screen.findByRole('button', { name: /expand row/i }))
    expect(await screen.findByTestId('audit-diff')).toBeInTheDocument()
    expect(screen.getByTestId('audit-diff-key-surveyId')).toBeInTheDocument()
    expect(screen.queryByTestId('audit-diff-key-questions')).not.toBeInTheDocument()
  })

  it('Audit tab Next button is disabled when there is no next page token', async () => {
    mockGetAuditLog.mockReturnValue({
      data: { entries: [{ id: 'a_1', action: 'playtest.edit', createdAt: '2026-05-01T10:00:00Z', beforeJson: '{}', afterJson: '{}' }], nextPageToken: '' },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Audit' }))
    const nextBtn = await screen.findByRole('button', { name: /^next$/i })
    expect(nextBtn).toBeDisabled()
  })

  it('Audit tab Next click advances the cursor to the returned next_page_token', async () => {
    mockGetAuditLog.mockReturnValue({
      data: {
        entries: [{ id: 'a_1', action: 'playtest.edit', createdAt: '2026-05-01T10:00:00Z', beforeJson: '{}', afterJson: '{}' }],
        nextPageToken: 'cursor-page-2'
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderDetail('autumn-draft')
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Audit' }))
    await user.click(await screen.findByRole('button', { name: /^next$/i }))
    await waitFor(() => {
      const lastCall = mockGetAuditLog.mock.calls.at(-1)
      expect(lastCall?.[1].queryParams.pageToken).toBe('cursor-page-2')
    })
  })
})
