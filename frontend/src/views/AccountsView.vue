<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 class="text-3xl font-bold text-ink">{{ t('ledger.title') }}</h1>
        <p class="text-sm text-ink-muted mt-1">{{ t('ledger.subtitle') }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <label for="ledger-month" class="sr-only">{{ t('ledger.month') }}</label>
        <input
          id="ledger-month"
          v-model="month"
          type="month"
          class="px-3 py-2 border border-line rounded-lg bg-surface text-ink text-sm"
        />
        <Button variant="secondary" size="sm" @click="editAccount(null)">+ {{ t('ledger.addAccount') }}</Button>
        <Button size="sm" @click="editEntry(null)">+ {{ t('ledger.addTransaction') }}</Button>
      </div>
    </div>

    <!-- Summary -->
    <section class="grid grid-cols-2 lg:grid-cols-4 gap-3" :aria-label="t('ledger.title')">
      <div class="col-span-2 lg:col-span-1 rounded-xl p-4 bg-ink text-canvas">
        <div class="text-xs opacity-75">{{ t('ledger.totalBalance') }} · {{ t('ledger.totalBalanceHint') }}</div>
        <div class="text-2xl font-bold tabular-nums mt-1">{{ money(totalBalance) }}</div>
      </div>
      <div class="rounded-xl p-4 bg-surface border border-line">
        <div class="text-xs text-ink-muted">{{ t('ledger.income') }}</div>
        <div class="text-lg sm:text-xl font-bold tabular-nums whitespace-nowrap mt-1 text-positive-soft">+{{ money(summary.income) }}</div>
      </div>
      <div class="rounded-xl p-4 bg-surface border border-line">
        <div class="text-xs text-ink-muted">{{ t('ledger.expense') }}</div>
        <div class="text-lg sm:text-xl font-bold tabular-nums whitespace-nowrap mt-1 text-danger">−{{ money(summary.expense) }}</div>
      </div>
      <div class="rounded-xl p-4 bg-surface border border-line">
        <div class="text-xs text-ink-muted">{{ t('ledger.net') }}</div>
        <div class="text-lg sm:text-xl font-bold tabular-nums whitespace-nowrap mt-1" :class="net >= 0 ? 'text-positive-soft' : 'text-danger'">
          {{ net >= 0 ? '+' : '−' }}{{ money(Math.abs(net)) }}
        </div>
      </div>
    </section>

    <!-- Accounts -->
    <section class="space-y-3">
      <h2 class="text-lg font-semibold text-ink">{{ t('ledger.accountsTitle') }}</h2>
      <p v-if="ledger.loaded && ledger.accounts.length === 0" class="text-sm text-ink-muted">{{ t('ledger.noAccounts') }}</p>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        <article
          v-for="a in ledger.accounts"
          :key="a.id"
          class="rounded-xl p-4 bg-surface border border-line space-y-2"
          :class="{ 'opacity-60': a.is_archived }"
        >
          <div class="flex items-start gap-3">
            <span class="w-10 h-10 rounded-lg grid place-items-center text-white text-xs font-bold shrink-0" :style="{ background: badgeColor(a) }">
              {{ badgeText(a) }}
            </span>
            <div class="min-w-0 flex-1">
              <div class="font-semibold text-ink truncate">{{ a.name }}</div>
              <div class="text-xs text-ink-muted truncate">
                {{ [a.bank, t(`ledger.types.${a.type}`)].filter(Boolean).join(' · ') }}
                <span v-if="a.is_archived"> · {{ t('ledger.archived') }}</span>
              </div>
            </div>
            <button type="button" class="text-xs font-semibold text-accent-soft hover:underline" @click="editAccount(a)">
              {{ t('ledger.edit') }}
            </button>
          </div>
          <div class="text-2xl font-bold tabular-nums" :class="a.balance < 0 ? 'text-danger' : 'text-ink'">
            {{ a.balance < 0 ? '−' : '' }}{{ money(Math.abs(a.balance)) }}
          </div>
          <div class="flex items-center justify-between gap-2 text-sm text-ink-soft">
            <span class="font-mono tabular-nums">{{ a.account_number ? (revealed[a.id] ? formatNumber(a.account_number) : maskNumber(a.account_number)) : t('ledger.noNumber') }}</span>
            <button v-if="a.account_number" type="button" class="text-xs font-semibold text-accent-soft hover:underline" @click="revealed[a.id] = !revealed[a.id]">
              {{ revealed[a.id] ? t('ledger.hideNumber') : t('ledger.showNumber') }}
            </button>
          </div>
          <div v-if="a.owner_member_id" class="text-xs text-ink-muted">
            {{ t('ledger.owner') }}: {{ ledger.memberName(a.owner_member_id) }}
          </div>
        </article>
      </div>
    </section>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Spending per member -->
      <section class="rounded-xl p-5 bg-surface border border-line space-y-4">
        <div>
          <h2 class="text-lg font-semibold text-ink">{{ t('ledger.byMemberTitle') }}</h2>
          <p class="text-xs text-ink-muted mt-1">{{ t('ledger.byMemberHint') }}</p>
        </div>
        <p v-if="summary.by_member.length === 0" class="text-sm text-ink-muted">{{ t('ledger.noSpending') }}</p>
        <div v-for="m in summary.by_member" :key="m.member_id" class="space-y-2">
          <div class="flex items-center justify-between gap-2">
            <span class="font-medium text-ink">{{ m.member_id ? ledger.memberName(m.member_id) : t('ledger.shared') }}</span>
            <span class="font-semibold tabular-nums text-ink">{{ money(m.total) }}</span>
          </div>
          <div class="h-2 rounded-full bg-surface-2 overflow-hidden">
            <div class="h-full rounded-full" :style="{ width: `${(m.total / maxMember) * 100}%`, background: memberColor(m.member_id) }" />
          </div>
          <ul class="text-xs text-ink-soft space-y-0.5 pl-1">
            <li v-for="c in m.categories.slice(0, 4)" :key="c.category_id" class="flex justify-between gap-2">
              <span class="truncate">{{ categoryName(c.category_id) }}</span>
              <span class="tabular-nums">{{ money(c.total) }}</span>
            </li>
          </ul>
        </div>
      </section>

      <!-- Incomes & transfers -->
      <section class="rounded-xl p-5 bg-surface border border-line space-y-3">
        <h2 class="text-lg font-semibold text-ink">{{ t('ledger.transactionsTitle') }}</h2>
        <p v-if="entries.length === 0" class="text-sm text-ink-muted">{{ t('ledger.noTransactions') }}</p>
        <ul class="divide-y divide-line">
          <li v-for="e in entries" :key="e.id" class="py-2.5 flex items-center gap-3">
            <span
              class="w-9 h-9 rounded-lg grid place-items-center font-bold shrink-0"
              :class="e.type === 'income' ? 'bg-positive/15 text-positive-soft' : 'bg-blue-500/15 text-blue-600 dark:text-blue-300'"
              aria-hidden="true"
            >{{ e.type === 'income' ? '+' : '⇄' }}</span>
            <button type="button" class="min-w-0 flex-1 text-left" @click="editEntry(e)">
              <div class="font-medium text-ink truncate">
                {{ e.description || (e.type === 'income' ? t(`ledger.incomeCategories.${e.category || 'other'}`) : t('ledger.txModal.transfer')) }}
              </div>
              <div class="text-xs text-ink-muted truncate">
                {{ formatDate(e.date) }} ·
                <template v-if="e.type === 'transfer'">{{ ledger.accountName(e.account_id) }} → {{ ledger.accountName(e.to_account_id) }}</template>
                <template v-else>{{ ledger.accountName(e.account_id) }}<span v-if="e.for_member_id"> · {{ ledger.memberName(e.for_member_id) }}</span></template>
              </div>
            </button>
            <span class="font-semibold tabular-nums whitespace-nowrap" :class="e.type === 'income' ? 'text-positive-soft' : 'text-ink-soft'">
              {{ e.type === 'income' ? '+' : '' }}{{ money(e.amount) }}
            </span>
            <button type="button" class="p-1.5 rounded-lg text-ink-faint hover:text-danger hover:bg-surface-2" :aria-label="t('ledger.delete')" @click="removeEntry(e)">
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
            </button>
          </li>
        </ul>
      </section>
    </div>

    <AccountModal v-if="accountModal.open" :account="accountModal.account" @close="accountModal.open = false" @saved="reload" />
    <TransactionModal v-if="entryModal.open" :entry="entryModal.entry" @close="entryModal.open = false" @saved="reload" />
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ledgerAPI, categoriesAPI } from '@/api/client'
import { useLedgerStore } from '@/stores/ledger'
import { useSettingsStore } from '@/stores/settings'
import { useConfirm } from '@/composables/useConfirm'
import { formatCurrency, formatDate as _formatDate } from '@/utils/dateFormatter'
import { categoryLabel } from '@/utils/categoryLabel'
import { apiErrorMessage } from '@/utils/apiError'
import Button from '@/components/common/Button.vue'
import AccountModal from '@/components/ledger/AccountModal.vue'
import TransactionModal from '@/components/ledger/TransactionModal.vue'

defineOptions({ name: 'AccountsView' })

const { t } = useI18n()
const ledger = useLedgerStore()
const settingsStore = useSettingsStore()
const { confirm } = useConfirm()

const now = new Date()
const month = ref(`${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`)
const summary = ref({ income: 0, expense: 0, by_member: [] })
const entries = ref([])
const categories = ref([])
const revealed = reactive({})
const accountModal = reactive({ open: false, account: null })
const entryModal = reactive({ open: false, entry: null })

const totalBalance = computed(() => ledger.accounts.filter(a => !a.is_archived).reduce((s, a) => s + a.balance, 0))
const net = computed(() => summary.value.income - summary.value.expense)
const maxMember = computed(() => Math.max(1, ...summary.value.by_member.map(m => m.total)))

const PALETTE = ['#2F6FD6', '#C0428A', '#D9861E', '#2A9A8E', '#7B5CD6', '#5B8C2A']
function memberColor(id) {
  if (!id) return 'rgb(var(--c-ink-faint))'
  const idx = ledger.members.findIndex(m => m.id === id)
  return PALETTE[(idx < 0 ? 0 : idx) % PALETTE.length]
}

const BANK_COLORS = { 'กสิกร': '#138F2D', 'kbank': '#138F2D', 'ไทยพาณิชย์': '#4E2A84', 'scb': '#4E2A84', 'กรุงเทพ': '#1E3F8F', 'bbl': '#1E3F8F', 'กรุงไทย': '#00A1E0', 'ktb': '#00A1E0', 'กรุงศรี': '#D4A017', 'ktc': '#C8102E', 'ttb': '#0050F0', 'ทีทีบี': '#0050F0', 'ออมสิน': '#E4007F' }
function badgeColor(a) {
  const key = Object.keys(BANK_COLORS).find(k => (a.bank || '').toLowerCase().includes(k))
  if (key) return BANK_COLORS[key]
  return { cash: '#6B7280', ewallet: '#0E9F6E', credit_card: '#B91C1C' }[a.type] || '#475569'
}
const BANK_SHORT = { 'กสิกร': 'K', 'ไทยพาณิชย์': 'SCB', 'กรุงเทพ': 'BBL', 'กรุงไทย': 'KTB', 'กรุงศรี': 'BAY', 'ทีทีบี': 'ttb', 'ออมสิน': 'GSB' }
function badgeText(a) {
  if (a.type === 'cash') return '฿'
  const bank = (a.bank || '').replace(/^ธนาคาร/, '').trim()
  const key = Object.keys(BANK_SHORT).find(k => bank.includes(k))
  if (key) return BANK_SHORT[key]
  return (bank || a.name).slice(0, 3).toUpperCase()
}
function maskNumber(no) {
  if (no.length >= 15) return `•••• ${no.slice(-4)}`
  return `xxx-x-x${no.slice(-4)}`
}
function formatNumber(no) {
  if (no.length === 10) return `${no.slice(0, 3)}-${no.slice(3, 4)}-${no.slice(4, 9)}-${no.slice(9)}`
  return no.replace(/(\d{4})(?=\d)/g, '$1 ')
}
function money(v) {
  return formatCurrency(v || 0, settingsStore.formatSettings)
}
function formatDate(d) {
  return _formatDate(d, settingsStore.formatSettings)
}
function categoryName(id) {
  const c = categories.value.find(x => x.id === id)
  return c ? categoryLabel(c) : ''
}

async function loadMonth() {
  if (!ledger.propertyId) return
  try {
    const [s, tx] = await Promise.all([
      ledgerAPI.summary(ledger.propertyId, month.value),
      ledgerAPI.transactions(ledger.propertyId, { month: month.value }),
    ])
    summary.value = { income: s.data.income || 0, expense: s.data.expense || 0, by_member: s.data.by_member || [] }
    entries.value = tx.data || []
  } catch (err) {
    window.$toast?.error(apiErrorMessage(err))
  }
}

async function reload() {
  await ledger.refreshAccounts()
  await loadMonth()
}

function editAccount(a) {
  accountModal.account = a
  accountModal.open = true
}
function editEntry(e) {
  entryModal.entry = e
  entryModal.open = true
}
async function removeEntry(e) {
  const ok = await confirm({
    title: t('ledger.deleteConfirmTitle'),
    message: t('ledger.deleteConfirmMessage'),
    confirmText: t('ledger.delete'),
    variant: 'danger',
  })
  if (!ok) return
  try {
    await ledgerAPI.deleteTransaction(e.id)
    window.$toast?.success(t('ledger.deleted'))
    await reload()
  } catch (err) {
    window.$toast?.error(apiErrorMessage(err))
  }
}

watch(month, loadMonth)

onMounted(async () => {
  try {
    await ledger.load(true)
  } catch (err) {
    window.$toast?.error(apiErrorMessage(err))
  }
  categoriesAPI.list().then(({ data }) => { categories.value = data || [] }).catch(() => {})
  loadMonth()
})
</script>
