import { computed, reactive, ref, type Ref } from 'vue'

/** 列表筛选：initQueryParams 复用 + 客户端关键字/日期过滤 */
export function useListQuery<T extends Record<string, unknown>>(init: T) {
  const initQueryParams = { ...init }
  const queryParams = reactive({ ...init }) as T
  const pageNum = ref(1)
  const pageSize = ref(15)

  function resetQuery() {
    Object.assign(queryParams, { ...initQueryParams })
    pageNum.value = 1
  }

  function handleQuery() {
    pageNum.value = 1
  }

  return { initQueryParams, queryParams, pageNum, pageSize, resetQuery, handleQuery }
}

export function filterByKeyword<R>(rows: R[] | null | undefined, keyword: string, fields: (keyof R)[]): R[] {
  const list = rows ?? []
  const q = keyword.trim().toLowerCase()
  if (!q) return list
  return list.filter((row) =>
    fields.some((f) => {
      const v = row[f]
      if (v == null) return false
      return String(v).toLowerCase().includes(q)
    }),
  )
}

export function filterByDateField<R>(
  rows: R[],
  field: keyof R,
  start: string,
  end: string,
): R[] {
  if (!start && !end) return rows
  const startTs = start ? Date.parse(start + 'T00:00:00') : Number.NEGATIVE_INFINITY
  const endTs = end ? Date.parse(end + 'T23:59:59.999') : Number.POSITIVE_INFINITY
  return rows.filter((row) => {
    const raw = row[field]
    if (raw == null) return false
    const t = typeof raw === 'number' ? raw : Date.parse(String(raw))
    if (Number.isNaN(t)) return false
    return t >= startTs && t <= endTs
  })
}

export function paginateRows<R>(rows: R[], pageNum: Ref<number>, pageSize: Ref<number>) {
  const total = computed(() => rows.length)
  const pageRows = computed(() => {
    const start = (pageNum.value - 1) * pageSize.value
    return rows.slice(start, start + pageSize.value)
  })
  // note: caller should pass computed filtered list via a wrapper
  return { total, pageRows }
}

export function useClientPage<R>(filtered: Ref<R[]>, pageSizeDefault = 15) {
  const pageNum = ref(1)
  const pageSize = ref(pageSizeDefault)
  const total = computed(() => filtered.value.length)
  const pageRows = computed(() => {
    const start = (pageNum.value - 1) * pageSize.value
    return filtered.value.slice(start, start + pageSize.value)
  })
  function resetPage() {
    pageNum.value = 1
  }
  return { pageNum, pageSize, total, pageRows, resetPage }
}
