import { describe, expect, it } from 'vitest'
import { coverageUnits, creditFormError, emptyCreditDraft } from '@/components/seller/creditForm'
import { freezeTimingText, resolveFreezeHours } from '@/components/seller/funds'
import { privacyBlocksListing, sellerUntested } from '@/components/seller/listingGate'

describe('卖家资金与未检测挂单', () => {
  it('优先使用冻结小时，旧天数只作缺省换算', () => {
    expect(resolveFreezeHours({ freeze_hours: 24, freeze_days: 7 })).toBe(24)
    expect(resolveFreezeHours({ freeze_hours: 0, freeze_days: 7 })).toBe(0)
    expect(resolveFreezeHours({ freeze_days: 2 })).toBe(48)
    expect(freezeTimingText(0)).toContain('不足 1 小时')
    expect(freezeTimingText(24)).toContain('24 小时')
  })

  it('没有明确 tested 不能当成检测通过，隐私失败不能挂单', () => {
    expect(sellerUntested({ detection_status: undefined })).toBe(true)
    expect(sellerUntested({ detection_status: 'tested' })).toBe(false)
    expect(sellerUntested({ is_reference: true })).toBe(false)
    expect(privacyBlocksListing({ privacy_ok: false })).toBe(true)
    expect(privacyBlocksListing({ privacy_ok: true })).toBe(false)
  })

  it('活动包表单同时校验人民币实付和美元额度', () => {
    const draft = emptyCreditDraft()
    expect(creditFormError(draft)).toContain('标题')
    draft.title = '本店额度'
    draft.price = '30'
    draft.face = '0'
    expect(creditFormError(draft)).toContain('美元额度')
    draft.face = '10.00'
    expect(creditFormError(draft)).toBe('')
    expect(coverageUnits(10, 7)).toBe(70)
  })
})
