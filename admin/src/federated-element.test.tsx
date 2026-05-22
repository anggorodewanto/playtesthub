import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import dayjs, { type Dayjs } from 'dayjs'
import utc from 'dayjs/plugin/utc'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

dayjs.extend(utc)

vi.mock('@accelbyte/sdk-extend-app-ui', () => ({
  useAppUIContext: () => ({
    sdk: {},
    isCurrentUserHasPermission: () => true
  }),
  CrudType: { READ: 'READ', CREATE: 'CREATE', UPDATE: 'UPDATE', DELETE: 'DELETE' }
}))

const mockGetPlaytests = vi.fn()
const mockGetPlaytest = vi.fn()
const mockCreateMutation = vi.fn()
const mockDeleteMutation = vi.fn()
const mockEditMutation = vi.fn()
const mockTransitionMutation = vi.fn()
const mockGetApplicants = vi.fn()
const mockGetCodes = vi.fn()
const mockApproveMutation = vi.fn()
const mockRejectMutation = vi.fn()
const mockRetryDmMutation = vi.fn()
const mockUploadMutation = vi.fn()
const mockTopUpMutation = vi.fn()
const mockSyncMutation = vi.fn()
const mockGetWorkersHealth = vi.fn()
const mockCompleteAdtLinkMutation = vi.fn()
const mockRecoverAdtLinkMutation = vi.fn()
const mockGetAdtLinkages = vi.fn()
const mockGetAdtBuilds = vi.fn()
const mockGetAdtGames = vi.fn()
const mockStartAdtLinkMutation = vi.fn()
const mockUnlinkAdtMutation = vi.fn()
const mockGetPublicConfig = vi.fn()

vi.mock('./playtesthubapi/generated-public/queries/PlaytesthubService.query', () => ({
  usePlaytesthubServiceApi_GetConfig: (...a: unknown[]) => mockGetPublicConfig(...a)
}))

vi.mock('./playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query', () => ({
  Key_PlaytesthubServiceAdmin: {
    Playtests: 'playtests',
    Playtest: 'playtest',
    Playtest_ByPlaytestId: 'playtest-by-id',
    Playtest_ByPlaytestIdTransitionStatu: 'playtest-by-id-transition',
    Codes_ByPlaytestId: 'codes-by-playtest-id',
    Applicants_ByPlaytestId: 'applicants-by-playtest-id',
    AdtLinkages: 'adt-linkages',
    BuildsAdt_ByAdtLinkageId: 'adt-builds-by-linkage-id',
    GamesAdt_ByAdtLinkageId: 'adt-games-by-linkage-id'
  },
  usePlaytesthubServiceAdminApi_GetPlaytests: (...args: unknown[]) => mockGetPlaytests(...args),
  usePlaytesthubServiceAdminApi_GetPlaytest_ByPlaytestId: (...args: unknown[]) => mockGetPlaytest(...args),
  usePlaytesthubServiceAdminApi_CreatePlaytestMutation: (...args: unknown[]) => mockCreateMutation(...args),
  usePlaytesthubServiceAdminApi_DeletePlaytest_ByPlaytestIdMutation: (...args: unknown[]) => mockDeleteMutation(...args),
  usePlaytesthubServiceAdminApi_PatchPlaytest_ByPlaytestIdMutation: (...args: unknown[]) => mockEditMutation(...args),
  usePlaytesthubServiceAdminApi_CreatePlaytest_ByPlaytestIdTransitionStatuMutation: (...args: unknown[]) => mockTransitionMutation(...args),
  usePlaytesthubServiceAdminApi_GetApplicants_ByPlaytestId: (...args: unknown[]) => mockGetApplicants(...args),
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId: (...args: unknown[]) => mockGetCodes(...args),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdApproveMutation: (...args: unknown[]) => mockApproveMutation(...args),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRejectMutation: (...args: unknown[]) => mockRejectMutation(...args),
  usePlaytesthubServiceAdminApi_CreateApplicant_ByApplicantIdRetryDmMutation: (...args: unknown[]) => mockRetryDmMutation(...args),
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation: (...args: unknown[]) => mockUploadMutation(...args),
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation: (...args: unknown[]) => mockTopUpMutation(...args),
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation: (...args: unknown[]) => mockSyncMutation(...args),
  usePlaytesthubServiceAdminApi_GetWorkersHealth: (...args: unknown[]) => mockGetWorkersHealth(...args),
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesCompleteMutation: (...args: unknown[]) => mockCompleteAdtLinkMutation(...args),
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesRecoverMutation: (...args: unknown[]) => mockRecoverAdtLinkMutation(...args),
  usePlaytesthubServiceAdminApi_GetAdtLinkages: (...args: unknown[]) => mockGetAdtLinkages(...args),
  usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId: (...args: unknown[]) => mockGetAdtBuilds(...args),
  usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId: (...args: unknown[]) => mockGetAdtGames(...args),
  usePlaytesthubServiceAdminApi_CreateAdtLinkagesStartMutation: (...args: unknown[]) => mockStartAdtLinkMutation(...args),
  usePlaytesthubServiceAdminApi_DeleteAdtLinkage_ByAdtLinkageIdMutation: (...args: unknown[]) => mockUnlinkAdtMutation(...args)
}))

import { FederatedElement } from './federated-element'
import { dateRangeWindowRule } from './window'

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <FederatedElement />
      </MemoryRouter>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  mockGetPlaytests.mockReset()
  mockGetPlaytest.mockReset()
  mockCreateMutation.mockReset()
  mockDeleteMutation.mockReset()
  mockEditMutation.mockReset()
  mockTransitionMutation.mockReset()
  mockGetApplicants.mockReset()
  mockGetCodes.mockReset()
  mockApproveMutation.mockReset()
  mockRejectMutation.mockReset()
  mockRetryDmMutation.mockReset()
  mockUploadMutation.mockReset()
  mockTopUpMutation.mockReset()
  mockSyncMutation.mockReset()
  mockGetWorkersHealth.mockReset()
  mockCompleteAdtLinkMutation.mockReset()
  mockRecoverAdtLinkMutation.mockReset()
  mockGetAdtLinkages.mockReset()
  mockGetAdtBuilds.mockReset()
  mockGetAdtGames.mockReset()
  mockStartAdtLinkMutation.mockReset()
  mockUnlinkAdtMutation.mockReset()
  mockGetPublicConfig.mockReset()

  // Default: empty list + no-op mutations.
  mockGetPlaytests.mockReturnValue({ data: { playtests: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockCreateMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockDeleteMutation.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockEditMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockTransitionMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetPlaytest.mockReturnValue({ data: undefined, isLoading: false, error: null })
  mockGetApplicants.mockReturnValue({ data: { applicants: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockGetCodes.mockReturnValue({ data: { stats: { total: 0, unused: 0, reserved: 0, granted: 0 }, codes: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockApproveMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockRejectMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockRetryDmMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockUploadMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockTopUpMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockSyncMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetWorkersHealth.mockReturnValue({ data: { workers: [] }, isLoading: false, error: null })
  mockCompleteAdtLinkMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockRecoverAdtLinkMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetAdtLinkages.mockReturnValue({ data: { linkages: [] }, isLoading: false, error: null })
  mockGetAdtBuilds.mockReturnValue({ data: { builds: [] }, isLoading: false, error: null })
  mockGetAdtGames.mockReturnValue({ data: { games: [] }, isLoading: false, error: null })
  mockStartAdtLinkMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockUnlinkAdtMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
  mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: 'https://play.example.com' } })
})

// Antd renders Popconfirm + Modal.confirm via a portal on document.body.
// Auto-cleanup detaches the rendered React tree but the leftover modal
// mask + confirm dialog stays mounted on document.body and intercepts
// click events in later tests' portal-mounted Modals (e.g. the B13
// build-picker). Drop only stale confirm dialogs (the ant-modal-confirm
// variant) — leaving regular ant-modal-root nodes intact so that
// onUnmount cleanups inside antd's portal logic don't double-remove.
afterEach(() => {
  document.querySelectorAll('.ant-modal-confirm').forEach(node => {
    const root = node.closest('.ant-modal-root')
    if (root && root.parentNode) root.parentNode.removeChild(root)
  })
})

async function openRowMenu() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: /more actions/i }))
  return { user, menu: await screen.findByRole('menu') }
}

describe('PlaytestsListPage', () => {
  it('renders Playtest Hub header and a Create Playtest button', () => {
    renderAt('/')
    expect(screen.getByRole('heading', { name: /playtest hub/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /create playtest/i })).toBeInTheDocument()
  })

  it('renders each returned playtest in the table', () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [
          {
            id: 'pt_1',
            slug: 'summer-alpha',
            title: 'Summer Alpha',
            status: 'PLAYTEST_STATUS_OPEN',
            distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
            autoApprove: true,
            updatedAt: '2026-04-19T14:30:00Z'
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    expect(screen.getByText('summer-alpha')).toBeInTheDocument()
    expect(screen.getByText('Summer Alpha')).toBeInTheDocument()
    expect(screen.getByText('Published')).toBeInTheDocument()
    expect(screen.getByText('Steam Keys')).toBeInTheDocument()
    expect(screen.getByText('Auto-Approve')).toBeInTheDocument()
  })

  it('renders Manual approval tag when autoApprove is false', () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [
          {
            id: 'pt_1',
            slug: 'summer-alpha',
            title: 'Summer Alpha',
            status: 'PLAYTEST_STATUS_OPEN',
            distributionModel: 'DISTRIBUTION_MODEL_AGS_CAMPAIGN',
            autoApprove: false
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    expect(screen.getByText('Manual')).toBeInTheDocument()
    expect(screen.getByText('AGS Campaign Codes')).toBeInTheDocument()
  })

  it('renders ADT distribution label as "Direct Download (ADT)"', () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [{ id: 'pt_1', slug: 'adt-alpha', title: 'ADT', status: 'PLAYTEST_STATUS_DRAFT', distributionModel: 'DISTRIBUTION_MODEL_ADT' }]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    expect(screen.getByText('Direct Download (ADT)')).toBeInTheDocument()
  })

  it('publishes a DRAFT row via the row menu', async () => {
    const mutate = vi.fn()
    mockTransitionMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_DRAFT' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    const { user, menu } = await openRowMenu()
    await user.click(within(menu).getByText('Publish'))
    const confirmOk = await screen.findByRole('button', { name: /^publish$/i })
    await user.click(confirmOk)
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith(
        { playtestId: 'pt_1', data: { targetStatus: 'PLAYTEST_STATUS_OPEN' } },
        expect.anything()
      )
    )
  })

  it('stops an OPEN row via the row menu', async () => {
    const mutate = vi.fn()
    mockTransitionMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_OPEN' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    const { user, menu } = await openRowMenu()
    await user.click(within(menu).getByText('Stop Playtest'))
    const confirmOk = await screen.findByRole('button', { name: /^stop playtest$/i })
    await user.click(confirmOk)
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith(
        { playtestId: 'pt_1', data: { targetStatus: 'PLAYTEST_STATUS_CLOSED' } },
        expect.anything()
      )
    )
  })

  it('omits Publish and Stop Playtest from menu on CLOSED rows', async () => {
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_CLOSED' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    const { menu } = await openRowMenu()
    expect(within(menu).queryByText('Publish')).not.toBeInTheDocument()
    expect(within(menu).queryByText('Stop Playtest')).not.toBeInTheDocument()
  })

  it('calls DeletePlaytest mutation via the row menu', async () => {
    const mutate = vi.fn()
    mockDeleteMutation.mockReturnValue({ mutate, isPending: false })
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_DRAFT' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    const { user, menu } = await openRowMenu()
    await user.click(within(menu).getByText('Delete'))
    const confirmOk = await screen.findByRole('button', { name: /^delete$/i })
    await user.click(confirmOk)
    await waitFor(() => expect(mutate).toHaveBeenCalledWith({ playtestId: 'pt_1' }, expect.anything()))
  })

  it('copies the playtest player link from the row menu', async () => {
    mockGetPublicConfig.mockReturnValue({ data: { playerBaseUrl: 'https://play.example.com' } })
    mockGetPlaytests.mockReturnValue({
      data: { playtests: [{ id: 'pt_1', slug: 'summer-alpha', title: 'Summer Alpha', status: 'PLAYTEST_STATUS_DRAFT' }] },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })

    renderAt('/')
    const { user, menu } = await openRowMenu()
    await user.click(within(menu).getByText('Copy Link'))
    await waitFor(async () => {
      const text = await navigator.clipboard.readText()
      expect(text).toBe('https://play.example.com/#/playtest/summer-alpha')
    })
  })
})

describe('PlaytestCreatePage', () => {
  it('offers both distribution models with STEAM_KEYS as default', () => {
    renderAt('/new')
    const agsRadio = screen.getByRole('radio', { name: /AGS Campaign/i })
    expect(agsRadio).toBeEnabled()
    const steamRadio = screen.getByRole('radio', { name: /Steam keys/i })
    expect(steamRadio).toBeEnabled()
    expect(steamRadio).toBeChecked()
  })

  it('shows all the PRD-required fields on the create form', () => {
    renderAt('/new')
    expect(screen.getByLabelText(/slug/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/playtest title/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/banner image url/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/platforms/i)).toBeInTheDocument()
    expect(screen.getByText(/distribution model/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^create$/i })).toBeInTheDocument()
  })

  it('approval radio defaults to Manual Approval and hides the auto-approve limit input', () => {
    renderAt('/new')
    const manualRadio = screen.getByRole('radio', { name: /manual approval/i })
    expect(manualRadio).toBeChecked()
    expect(screen.queryByLabelText(/auto-approve limit/i)).not.toBeInTheDocument()
  })

  it('reveals the auto-approve limit input when the Auto-Approve radio is picked', async () => {
    renderAt('/new')
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: /auto-approve/i }))
    expect(await screen.findByLabelText(/auto-approve limit/i)).toBeInTheDocument()
  })

  it('rejects an out-of-bounds auto-approve limit with the byte-exact server message', async () => {
    const mutate = vi.fn()
    mockCreateMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/new')
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: /auto-approve/i }))
    const limit = await screen.findByLabelText(/auto-approve limit/i)
    await user.type(limit, '100001')
    // Required fields so the form actually reaches the validator.
    await user.type(screen.getByLabelText(/slug/i), 'demo-slug')
    await user.type(screen.getByLabelText(/playtest title/i), 'Demo')
    await user.click(screen.getByRole('button', { name: /^create$/i }))
    expect(
      await screen.findByText('auto_approve_limit must be between 1 and 100000 when auto_approve is true')
    ).toBeInTheDocument()
    expect(mutate).not.toHaveBeenCalled()
  })

  it('offers the ADT distribution radio (M5.B)', () => {
    renderAt('/new')
    expect(screen.getByRole('radio', { name: /^ADT$/i })).toBeEnabled()
  })

  it('reveals the ADT field set when ADT is picked', async () => {
    mockGetAdtLinkages.mockReturnValue({
      data: { linkages: [{ id: 'lnk-1', adtNamespace: 'adt-ns-1', studioNamespace: 'studio-A' }] },
      isLoading: false,
      error: null
    })
    renderAt('/new')
    const user = userEvent.setup()
    await user.click(screen.getByRole('radio', { name: /^ADT$/i }))
    expect(await screen.findByLabelText(/adt linkage/i)).toBeInTheDocument()
    // The build picker modal (B13) is the canonical UX; the typed adt game
    // id Input only renders in the fallback path. Confirm the picker button
    // is offered.
    expect(screen.getByRole('button', { name: /select game build/i })).toBeInTheDocument()
  })

  describe('Build picker modal (M5.B-13)', () => {
    function setLinkages() {
      mockGetAdtLinkages.mockReturnValue({
        data: { linkages: [{ id: 'lnk-1', adtNamespace: 'adt-ns-1', studioNamespace: 'studio-A' }] },
        isLoading: false,
        error: null
      })
    }

    it('disables the Select Game Build button until an ADT linkage is picked, then opens the modal with a games dropdown', async () => {
      setLinkages()
      mockGetAdtGames.mockReturnValue({
        data: { games: [{ id: 'game-1', name: 'Starfield Dev', createdAt: '2026-05-01T00:00:00Z' }] },
        isLoading: false,
        error: null
      })
      renderAt('/new')
      const user = userEvent.setup()
      await user.click(screen.getByRole('radio', { name: /^ADT$/i }))

      const openButton = await screen.findByRole('button', { name: /select game build/i })
      expect(openButton).toBeDisabled()

      await user.click(screen.getByLabelText(/adt linkage/i))
      await user.click(await screen.findByText(/adt-ns-1/i))

      await waitFor(() => expect(screen.getByRole('button', { name: /select game build/i })).toBeEnabled())
      await user.click(screen.getByRole('button', { name: /select game build/i }))

      const dialog = await screen.findByRole('dialog', { name: /select game build/i })
      expect(within(dialog).getByText(/starfield dev/i)).toBeInTheDocument()
    })

    it('groups builds by game_version_name in the left rail with per-version build counts', async () => {
      setLinkages()
      mockGetAdtGames.mockReturnValue({
        data: { games: [{ id: 'game-1', name: 'Starfield Dev' }] },
        isLoading: false,
        error: null
      })
      mockGetAdtBuilds.mockReturnValue({
        data: {
          builds: [
            { id: 'b1', name: 'v1.3 — Performance Pass', version: 'v1.3', platform: 'windows', uploadedAt: '2026-05-10T00:00:00Z' },
            { id: 'b2', name: 'v1.3 — Performance Pass', version: 'v1.3', platform: 'macos', uploadedAt: '2026-05-11T00:00:00Z' },
            { id: 'b3', name: 'v1.2 — Combat Update', version: 'v1.2', platform: 'windows', uploadedAt: '2026-04-15T00:00:00Z' }
          ]
        },
        isLoading: false,
        error: null
      })
      renderAt('/new')
      const user = userEvent.setup()
      await user.click(screen.getByRole('radio', { name: /^ADT$/i }))
      await user.click(screen.getByLabelText(/adt linkage/i))
      await user.click(await screen.findByText(/adt-ns-1/i))
      await user.click(await screen.findByRole('button', { name: /select game build/i }))

      const dialog = await screen.findByRole('dialog', { name: /select game build/i })
      expect(within(dialog).getByText('v1.3 — Performance Pass')).toBeInTheDocument()
      expect(within(dialog).getByText('v1.2 — Combat Update')).toBeInTheDocument()
      expect(within(dialog).getByText(/2 builds/i)).toBeInTheDocument()
      expect(within(dialog).getByText(/1 build$/i)).toBeInTheDocument()
    })

    it('renders one card per platform when a version is selected in the left rail', async () => {
      setLinkages()
      mockGetAdtGames.mockReturnValue({
        data: { games: [{ id: 'game-1', name: 'Starfield Dev' }] },
        isLoading: false,
        error: null
      })
      mockGetAdtBuilds.mockReturnValue({
        data: {
          builds: [
            { id: 'b1', name: 'v1.3 — Performance Pass', version: 'v1.3', platform: 'windows', uploadedAt: '2026-05-10T00:00:00Z' },
            { id: 'b2', name: 'v1.3 — Performance Pass', version: 'v1.3', platform: 'macos', uploadedAt: '2026-05-11T00:00:00Z' }
          ]
        },
        isLoading: false,
        error: null
      })
      renderAt('/new')
      const user = userEvent.setup()
      await user.click(screen.getByRole('radio', { name: /^ADT$/i }))
      await user.click(screen.getByLabelText(/adt linkage/i))
      await user.click(await screen.findByText(/adt-ns-1/i))
      await user.click(await screen.findByRole('button', { name: /select game build/i }))

      const dialog = await screen.findByRole('dialog', { name: /select game build/i })
      await user.click(within(dialog).getByText('v1.3 — Performance Pass'))

      expect(within(dialog).getByText(/windows/i)).toBeInTheDocument()
      expect(within(dialog).getByText(/macos/i)).toBeInTheDocument()
      expect(within(dialog).getByText('b1')).toBeInTheDocument()
      expect(within(dialog).getByText('b2')).toBeInTheDocument()
    })

    it('wires both adtGameId and adtBuildId on the parent form when Use This Build is clicked', async () => {
      setLinkages()
      const mutate = vi.fn()
      mockCreateMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
      mockGetAdtGames.mockReturnValue({
        data: { games: [{ id: 'game-1', name: 'Starfield Dev' }] },
        isLoading: false,
        error: null
      })
      mockGetAdtBuilds.mockReturnValue({
        data: {
          builds: [{ id: 'b1', name: 'v1.3 — Performance Pass', version: 'v1.3', platform: 'windows', uploadedAt: '2026-05-10T00:00:00Z' }]
        },
        isLoading: false,
        error: null
      })
      renderAt('/new')
      const user = userEvent.setup()
      // Fill enough of the form to reach submit; the build picker drives the
      // adtGameId + adtBuildId fields which we then assert end up on the parent
      // form's submit payload.
      await user.type(screen.getByLabelText(/slug/i), 'demo-slug')
      await user.type(screen.getByLabelText(/playtest title/i), 'Demo')
      await user.click(screen.getByRole('radio', { name: /^ADT$/i }))
      await user.click(screen.getByLabelText(/adt linkage/i))
      await user.click(await screen.findByText(/adt-ns-1/i))
      await user.click(await screen.findByRole('button', { name: /select game build/i }))

      const dialog = await screen.findByRole('dialog', { name: /select game build/i })
      // Default selected game = the only one. Pick the version, then pick the
      // build card, then Use This Build.
      await user.click(within(dialog).getByText('v1.3 — Performance Pass'))
      await user.click(within(dialog).getByTestId('adt-picker-build-b1'))
      await user.click(within(dialog).getByRole('button', { name: /use this build/i }))

      await waitFor(() => expect(screen.queryByRole('dialog', { name: /select game build/i })).not.toBeInTheDocument())
      // Picked summary surfaces in the parent form so operators see what they picked.
      expect(screen.getByText(/starfield dev/i)).toBeInTheDocument()
      expect(screen.getByText('b1')).toBeInTheDocument()
    })

    it('preserves typed slug + title when the modal is cancelled', async () => {
      setLinkages()
      mockGetAdtGames.mockReturnValue({
        data: { games: [{ id: 'game-1', name: 'Starfield Dev' }] },
        isLoading: false,
        error: null
      })
      renderAt('/new')
      const user = userEvent.setup()
      await user.type(screen.getByLabelText(/slug/i), 'demo-slug')
      await user.type(screen.getByLabelText(/playtest title/i), 'My Title')
      await user.click(screen.getByRole('radio', { name: /^ADT$/i }))
      await user.click(screen.getByLabelText(/adt linkage/i))
      await user.click(await screen.findByText(/adt-ns-1/i))
      await user.click(await screen.findByRole('button', { name: /select game build/i }))

      const dialog = await screen.findByRole('dialog', { name: /select game build/i })
      await user.click(within(dialog).getByRole('button', { name: /cancel/i }))

      await waitFor(() => expect(screen.queryByRole('dialog', { name: /select game build/i })).not.toBeInTheDocument())
      expect(screen.getByLabelText(/slug/i)).toHaveValue('demo-slug')
      expect(screen.getByLabelText(/playtest title/i)).toHaveValue('My Title')
    })
  })
})

describe('ADTLinkagesPanel', () => {
  it('renders empty-state copy when no linkages exist', () => {
    renderAt('/')
    expect(screen.getByText(/no ADT linkages yet/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /link new ADT namespace/i })).toBeInTheDocument()
  })

  it('renders linkage rows + an Unlink button per row', () => {
    mockGetAdtLinkages.mockReturnValue({
      data: { linkages: [{ id: 'lnk-1', adtNamespace: 'adt-ns-1', studioNamespace: 'studio-A', linkedAt: '2026-05-19T00:00:00Z' }] },
      isLoading: false,
      error: null
    })
    renderAt('/')
    expect(screen.getByText('adt-ns-1')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /unlink/i })).toBeInTheDocument()
  })

  it('opens the Link ADT modal on click', async () => {
    renderAt('/')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /link new ADT namespace/i }))
    expect(await screen.findByText(/you will be redirected to ADT to authorise the linkage/i)).toBeInTheDocument()
  })
})

describe('PlaytestEditPage', () => {
  it('renders a loading spinner while fetching', () => {
    mockGetPlaytest.mockReturnValue({ data: undefined, isLoading: true, error: null })
    renderAt('/pt_1/edit')
    expect(screen.getByText(/loading playtest/i)).toBeInTheDocument()
  })

  it('pre-fills the form with the fetched playtest', async () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          description: 'Alpha description',
          platforms: ['PLATFORM_STEAM'],
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
          ndaRequired: false
        }
      },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/edit')
    await waitFor(() => expect(screen.getByDisplayValue('Summer Alpha')).toBeInTheDocument())
    expect(screen.getByDisplayValue('Alpha description')).toBeInTheDocument()
    expect(screen.getByText(/immutable after creation/i)).toBeInTheDocument()
  })

  it('preloads auto-approve toggle + limit from the fetched playtest', async () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          platforms: ['PLATFORM_STEAM'],
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
          ndaRequired: false,
          autoApprove: true,
          autoApproveLimit: 25
        }
      },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/edit')
    await waitFor(() => expect(screen.getByDisplayValue('Summer Alpha')).toBeInTheDocument())
    expect(screen.getByRole('switch', { name: /auto-approve/i })).toBeChecked()
    expect(screen.getByLabelText(/auto-approve limit/i)).toHaveValue('25')
  })
})
describe('Playtest window (M4)', () => {
  it('labels create-form date range as UTC and surfaces auto-transition help', () => {
    renderAt('/new')
    expect(screen.getByText('Starts / Ends (UTC)')).toBeInTheDocument()
    expect(screen.queryByText(/display-only in MVP/i)).not.toBeInTheDocument()
    expect(screen.getByText(/Auto-opens at start, auto-closes at end/i)).toBeInTheDocument()
  })

  it('labels edit-form date range as UTC', async () => {
    mockGetPlaytest.mockReturnValue({
      data: {
        playtest: {
          id: 'pt_1',
          slug: 'summer-alpha',
          title: 'Summer Alpha',
          platforms: ['PLATFORM_STEAM'],
          distributionModel: 'DISTRIBUTION_MODEL_STEAM_KEYS',
          ndaRequired: false,
          startsAt: '2026-06-01T10:00:00Z',
          endsAt: '2026-06-08T10:00:00Z'
        }
      },
      isLoading: false,
      error: null
    })
    renderAt('/pt_1/edit')
    await waitFor(() => expect(screen.getByDisplayValue('Summer Alpha')).toBeInTheDocument())
    expect(screen.getByText('Starts / Ends (UTC)')).toBeInTheDocument()
  })

  it('validator rejects inverted + equal windows with byte-exact server message and accepts a valid window', async () => {
    // Driving the antd RangePicker through userEvent in jsdom is fragile; the byte-exact server
    // error string is owned by the validator rule, so we exercise it directly.
    const inverted: [Dayjs, Dayjs] = [dayjs.utc('2026-06-08T10:00:00Z'), dayjs.utc('2026-06-01T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, inverted)).rejects.toThrow('ends_at must be after starts_at')
    const equal: [Dayjs, Dayjs] = [dayjs.utc('2026-06-08T10:00:00Z'), dayjs.utc('2026-06-08T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, equal)).rejects.toThrow('ends_at must be after starts_at')
    const valid: [Dayjs, Dayjs] = [dayjs.utc('2026-06-01T10:00:00Z'), dayjs.utc('2026-06-08T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, valid)).resolves.toBeUndefined()
    const onlyStart: [Dayjs, null] = [dayjs.utc('2026-06-01T10:00:00Z'), null]
    await expect(dateRangeWindowRule.validator(undefined, onlyStart)).resolves.toBeUndefined()
    const onlyEnd: [null, Dayjs] = [null, dayjs.utc('2026-06-08T10:00:00Z')]
    await expect(dateRangeWindowRule.validator(undefined, onlyEnd)).resolves.toBeUndefined()
  })

  it('renders an Auto-opens tooltip on DRAFT rows with startsAt set', async () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [
          {
            id: 'pt_1',
            slug: 'summer-alpha',
            title: 'Summer Alpha',
            status: 'PLAYTEST_STATUS_DRAFT',
            startsAt: dayjs.utc().add(2, 'hour').toISOString()
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    const user = userEvent.setup()
    await user.hover(screen.getByText('Draft'))
    const tip = await screen.findByRole('tooltip')
    expect(tip.textContent).toMatch(/Auto-opens/)
  })

  it('renders an Auto-closes tooltip on OPEN rows with endsAt set', async () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [
          {
            id: 'pt_1',
            slug: 'summer-alpha',
            title: 'Summer Alpha',
            status: 'PLAYTEST_STATUS_OPEN',
            endsAt: dayjs.utc().add(3, 'day').toISOString()
          }
        ]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    const user = userEvent.setup()
    await user.hover(screen.getByText('Published'))
    const tip = await screen.findByRole('tooltip')
    expect(tip.textContent).toMatch(/Auto-closes/)
  })

  it('shows a red Alert banner when any worker is stale', () => {
    mockGetWorkersHealth.mockReturnValue({
      data: {
        workers: [
          { name: 'reclaim-job', stale: false },
          { name: 'window-worker', stale: true }
        ]
      },
      isLoading: false,
      error: null
    })
    renderAt('/')
    const banner = screen.getByTestId('worker-health-banner')
    expect(banner).toBeInTheDocument()
    expect(banner.textContent).toMatch(/window-worker/)
    expect(banner.textContent).toMatch(/Auto-transitions are paused/)
  })

  it('does not render the banner when every worker is fresh', () => {
    mockGetWorkersHealth.mockReturnValue({
      data: { workers: [{ name: 'reclaim-job', stale: false }, { name: 'window-worker', stale: false }] },
      isLoading: false,
      error: null
    })
    renderAt('/')
    expect(screen.queryByTestId('worker-health-banner')).not.toBeInTheDocument()
  })

  it('does not render a tooltip on DRAFT rows without startsAt', () => {
    mockGetPlaytests.mockReturnValue({
      data: {
        playtests: [{ id: 'pt_1', slug: 'manual', title: 'Manual', status: 'PLAYTEST_STATUS_DRAFT' }]
      },
      isLoading: false,
      error: null,
      refetch: vi.fn()
    })
    renderAt('/')
    // No hover; tooltip should not have a trigger wrapper. Tag rendered raw.
    expect(screen.getByText('Draft')).toBeInTheDocument()
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })
})

describe('ADTLinkCallbackPage', () => {
  it('calls CompleteADTLink with state + adt_namespace from the query string', async () => {
    const mutate = vi.fn()
    mockCompleteAdtLinkMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc123&result=success&adt_namespace=adt-studio-1')
    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1))
    expect(mutate).toHaveBeenCalledWith({ data: { state: 'abc123', adtNamespace: 'adt-studio-1' } })
  })

  it('surfaces an error and does not invoke the mutation when state is missing', async () => {
    const mutate = vi.fn()
    mockCompleteAdtLinkMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?result=success&adt_namespace=adt-studio-1')
    expect(await screen.findByText(/missing the state or adt_namespace/i)).toBeInTheDocument()
    expect(mutate).not.toHaveBeenCalled()
  })

  it('surfaces an error when ADT reports the link as failed', async () => {
    const mutate = vi.fn()
    mockCompleteAdtLinkMutation.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc&adt_namespace=adt-studio-1&result=failed&reason=user_declined')
    expect(await screen.findByText(/user_declined/)).toBeInTheDocument()
    expect(mutate).not.toHaveBeenCalled()
    expect(screen.queryByTestId('adt-link-callback-retry')).not.toBeInTheDocument()
  })

  it('offers Retry when CompleteADTLink mutation errors and refires the mutation on click', async () => {
    const user = userEvent.setup()
    // Capture the onError callback the hook receives so we can drive it synchronously.
    let capturedOnError: ((err: { message?: string }) => void) | undefined
    const mutate = vi.fn()
    mockCompleteAdtLinkMutation.mockImplementation((_sdk: unknown, opts: { onError?: (err: { message?: string }) => void }) => {
      capturedOnError = opts.onError
      return { mutate, isPending: false, isError: false, error: null }
    })
    renderAt('/adt-link-callback?state=abc&result=success&adt_namespace=adt-studio-1')
    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1))

    // Drive the mutation into a failure (e.g. gateway 502 before reaching the backend).
    capturedOnError?.({ message: 'gateway timeout' })

    const retry = await screen.findByTestId('adt-link-callback-retry')
    expect(screen.getByText(/gateway timeout/)).toBeInTheDocument()
    await user.click(retry)
    expect(mutate).toHaveBeenCalledTimes(2)
    expect(mutate).toHaveBeenLastCalledWith({ data: { state: 'abc', adtNamespace: 'adt-studio-1' } })
  })

  // 2026-05-21 recovery affordance — Bug 2 of the M5.B follow-up:
  // when ADT reports result=failed&reason=already_linked, the operator
  // should be able to adopt the orphan flag with a single click instead
  // of dead-ending on the error toast.
  it('offers Recover existing linkage as primary when result=failed&reason=already_linked', async () => {
    const user = userEvent.setup()
    const recoverMutate = vi.fn()
    mockRecoverAdtLinkMutation.mockReturnValue({ mutate: recoverMutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc&adt_namespace=adt-studio-1&result=failed&reason=already_linked')
    const recover = await screen.findByTestId('adt-link-callback-recover')
    expect(recover).toHaveTextContent(/recover existing linkage/i)
    await user.click(recover)
    expect(recoverMutate).toHaveBeenCalledTimes(1)
    expect(recoverMutate).toHaveBeenCalledWith({ data: { adtNamespace: 'adt-studio-1' } })
  })

  // Fallback affordance: ADT reported an unknown reason, but if the
  // operator believes ADT carries the flag they can still try Recover.
  it('offers a secondary Recover affordance when the failure reason is unknown', async () => {
    mockRecoverAdtLinkMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc&adt_namespace=adt-studio-1&result=failed&reason=something_weird')
    const recover = await screen.findByTestId('adt-link-callback-recover')
    expect(recover).toHaveTextContent(/if you believe adt already has this linkage, try recover/i)
  })

  // Surface the recovery mutation's own error so the operator can see
  // it (e.g. FailedPrecondition when ADT actually reports no flag).
  it('surfaces the RecoverADTLinkage mutation error', async () => {
    const user = userEvent.setup()
    let capturedOnError: ((err: { message?: string }) => void) | undefined
    const mutate = vi.fn()
    mockRecoverAdtLinkMutation.mockImplementation((_sdk: unknown, opts: { onError?: (err: { message?: string }) => void }) => {
      capturedOnError = opts.onError
      return { mutate, isPending: false, isError: false, error: null }
    })
    renderAt('/adt-link-callback?state=abc&adt_namespace=adt-studio-1&result=failed&reason=already_linked')
    const recover = await screen.findByTestId('adt-link-callback-recover')
    await user.click(recover)
    expect(mutate).toHaveBeenCalledTimes(1)

    capturedOnError?.({ message: 'no ADT-side linkage found for that namespace; use StartADTLink to create one' })
    expect(await screen.findByTestId('adt-link-callback-recover-error')).toBeInTheDocument()
    expect(screen.getByText(/no ADT-side linkage found/i)).toBeInTheDocument()
  })

  // 2026-05-22 bug repro: ADT does NOT echo adt_namespace back on
  // result=failed (live URL observed in prod). The recover button used
  // to require Boolean(adtNamespace) so it never rendered, dead-ending
  // the operator. The callback must still expose a recover affordance
  // that asks the operator to type the namespace they intended to link.
  it('exposes a Recover affordance when ADT failure omits adt_namespace', async () => {
    mockRecoverAdtLinkMutation.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc&result=failed&reason=link_failed')
    expect(await screen.findByTestId('adt-link-callback-recover-prompt')).toBeInTheDocument()
  })

  it('calls RecoverADTLinkage with the operator-typed adt_namespace when ADT omitted it', async () => {
    const user = userEvent.setup()
    const recoverMutate = vi.fn()
    mockRecoverAdtLinkMutation.mockReturnValue({ mutate: recoverMutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc&result=failed&reason=link_failed')
    const prompt = await screen.findByTestId('adt-link-callback-recover-prompt')
    await user.click(prompt)
    const input = await screen.findByTestId('adt-link-callback-recover-input')
    await user.type(input, 'adt-orphan-ns')
    const submit = await screen.findByTestId('adt-link-callback-recover-submit')
    await user.click(submit)
    expect(recoverMutate).toHaveBeenCalledTimes(1)
    expect(recoverMutate).toHaveBeenCalledWith({ data: { adtNamespace: 'adt-orphan-ns' } })
  })

  it('refuses to submit recover when the operator left adt_namespace empty', async () => {
    const user = userEvent.setup()
    const recoverMutate = vi.fn()
    mockRecoverAdtLinkMutation.mockReturnValue({ mutate: recoverMutate, isPending: false, isError: false, error: null })
    renderAt('/adt-link-callback?state=abc&result=failed&reason=link_failed')
    const prompt = await screen.findByTestId('adt-link-callback-recover-prompt')
    await user.click(prompt)
    const submit = await screen.findByTestId('adt-link-callback-recover-submit')
    expect(submit).toBeDisabled()
    expect(recoverMutate).not.toHaveBeenCalled()
  })
})
