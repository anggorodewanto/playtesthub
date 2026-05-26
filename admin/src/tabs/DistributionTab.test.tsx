import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@accelbyte/sdk-extend-app-ui', () => ({
  useAppUIContext: () => ({ sdk: {}, isCurrentUserHasPermission: () => true }),
  CrudType: { READ: 'READ', CREATE: 'CREATE', UPDATE: 'UPDATE', DELETE: 'DELETE' }
}))

const mockGetCodes = vi.fn()
const mockUploadCodes = vi.fn()
const mockTopUpCodes = vi.fn()
const mockSyncCodes = vi.fn()
const mockGetAdtLinkages = vi.fn()
const mockGetAdtGames = vi.fn()
const mockGetAdtBuilds = vi.fn()
const mockChangeAdtBuild = vi.fn()

vi.mock('../playtesthubapi/generated-admin/queries/PlaytesthubServiceAdmin.query', () => ({
  Key_PlaytesthubServiceAdmin: {
    Playtests: 'playtests',
    Codes_ByPlaytestId: 'codes-by-playtest-id',
    GamesAdt_ByAdtLinkageId: 'adt-games-by-linkage-id',
    BuildsAdt_ByAdtLinkageId: 'adt-builds-by-linkage-id',
    AdtBuildChange_ByPlaytestId: 'adt-build-change-by-playtest-id'
  },
  usePlaytesthubServiceAdminApi_GetCodes_ByPlaytestId: (...a: unknown[]) => mockGetCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesUpload_ByPlaytestIdMutation: (...a: unknown[]) => mockUploadCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesTopUp_ByPlaytestIdMutation: (...a: unknown[]) => mockTopUpCodes(...a),
  usePlaytesthubServiceAdminApi_CreateCodesSyncFromAg_ByPlaytestIdMutation: (...a: unknown[]) => mockSyncCodes(...a),
  usePlaytesthubServiceAdminApi_GetAdtLinkages: (...a: unknown[]) => mockGetAdtLinkages(...a),
  usePlaytesthubServiceAdminApi_GetGamesAdt_ByAdtLinkageId: (...a: unknown[]) => mockGetAdtGames(...a),
  usePlaytesthubServiceAdminApi_GetBuildsAdt_ByAdtLinkageId: (...a: unknown[]) => mockGetAdtBuilds(...a),
  usePlaytesthubServiceAdminApi_CreateAdtBuildChange_ByPlaytestIdMutation: (...a: unknown[]) => mockChangeAdtBuild(...a)
}))

// Stub the build picker so its onPick prop can be driven deterministically
// without exercising the full namespace → game → version → build UI (that
// flow is covered against the real component in federated-element.test.tsx).
let capturedOnPick: ((gameId: string, buildId: string) => void) | undefined
vi.mock('../shared/adt-build-picker', () => ({
  ADTBuildPickerModal: (props: { open: boolean; onPick: (gameId: string, buildId: string) => void }) => {
    capturedOnPick = props.onPick
    if (!props.open) return null
    return <div data-testid="adt-build-picker-modal">picker</div>
  }
}))

import { DistributionTab } from './DistributionTab'

const ADT_PT = {
  id: 'pt-adt',
  slug: 'autumn-adt',
  title: 'Autumn ADT',
  distributionModel: 'DISTRIBUTION_MODEL_ADT',
  adtNamespace: 'adt-ns-1',
  adtGameId: 'game-1',
  adtBuildId: 'build-1'
}

function renderTab(playtest: Record<string, unknown>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <DistributionTab playtest={playtest} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  capturedOnPick = undefined
  mockGetCodes.mockReturnValue({ data: { stats: { total: 0, unused: 0, granted: 0 }, codes: [] }, isLoading: false, error: null, refetch: vi.fn() })
  mockUploadCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockTopUpCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockSyncCodes.mockReturnValue({ mutate: vi.fn(), isPending: false })
  mockGetAdtLinkages.mockReturnValue({
    data: { linkages: [{ id: 'lnk-1', adtNamespace: 'adt-ns-1', studioNamespace: 'studio-A', linkedAt: '2026-05-19T00:00:00Z' }] },
    isLoading: false,
    error: null,
    refetch: vi.fn()
  })
  mockGetAdtGames.mockReturnValue({ data: { games: [{ id: 'game-1', name: 'Starfield Dev' }] }, isLoading: false, error: null })
  mockGetAdtBuilds.mockReturnValue({ data: { builds: [] }, isLoading: false, error: null })
  mockChangeAdtBuild.mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, error: null })
})

describe('ADTPanel (Change Build)', () => {
  it('does not render an Unlink button on a linked ADT playtest', () => {
    renderTab(ADT_PT)
    expect(screen.queryByRole('button', { name: /unlink/i })).not.toBeInTheDocument()
    expect(screen.getByText('● Connected')).toBeInTheDocument()
  })

  it('opens the build picker modal when Change Build is clicked', async () => {
    renderTab(ADT_PT)
    const user = userEvent.setup()
    expect(screen.queryByTestId('adt-build-picker-modal')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /change build/i }))
    expect(await screen.findByTestId('adt-build-picker-modal')).toBeInTheDocument()
  })

  it('calls ChangeADTBuild with playtestId + adtGameId/adtBuildId when onPick fires', async () => {
    const mutate = vi.fn()
    mockChangeAdtBuild.mockReturnValue({ mutate, isPending: false, isError: false, error: null })
    renderTab(ADT_PT)
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /change build/i }))
    capturedOnPick?.('game-2', 'build-9')
    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({ playtestId: 'pt-adt', data: { adtGameId: 'game-2', adtBuildId: 'build-9' } })
    )
  })
})
