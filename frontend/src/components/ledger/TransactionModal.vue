<template>
  <BaseModal :show="true" :title="entry ? t('ledger.txModal.editTitle') : t('ledger.txModal.title')" @close="$emit('close')">
    <form class="space-y-4" @submit.prevent="save">
      <div class="grid grid-cols-2 gap-1 p-1 rounded-xl bg-surface-2" role="group" :aria-label="t('ledger.txModal.title')">
        <button
          v-for="kind in ['income', 'transfer']"
          :key="kind"
          type="button"
          :aria-pressed="form.type === kind"
          class="py-2 rounded-lg text-sm font-semibold transition-colors"
          :class="form.type === kind
            ? (kind === 'income' ? 'bg-positive text-white' : 'bg-blue-600 text-white')
            : 'text-ink-soft hover:bg-surface'"
          @click="form.type = kind"
        >
          {{ t(`ledger.txModal.${kind}`) }}
        </button>
      </div>

      <p v-if="blocker" class="text-sm text-warning-soft">{{ blocker }}</p>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <Input id="tx-amount" v-model="form.amount" :label="t('ledger.txModal.amount')" type="text" inputmode="decimal" autocomplete="off" required />
        <Input id="tx-date" v-model="form.date" :label="t('ledger.txModal.date')" type="date" required />
      </div>
      <Input
        id="tx-description"
        v-model="form.description"
        :label="t('ledger.txModal.description')"
        :placeholder="t('ledger.txModal.descriptionPlaceholder')"
      />

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label for="tx-account" class="block text-sm text-ink-soft mb-1">
            {{ form.type === 'income' ? t('ledger.txModal.account') : t('ledger.txModal.fromAccount') }}
          </label>
          <select id="tx-account" v-model="form.account_id" :class="selectClass">
            <option v-for="a in pickable" :key="a.id" :value="a.id">{{ ledger.accountName(a.id) }}</option>
          </select>
        </div>
        <div v-if="form.type === 'transfer'">
          <label for="tx-to" class="block text-sm text-ink-soft mb-1">{{ t('ledger.txModal.toAccount') }}</label>
          <select id="tx-to" v-model="form.to_account_id" :class="selectClass">
            <option v-for="a in pickable" :key="a.id" :value="a.id">{{ ledger.accountName(a.id) }}</option>
          </select>
        </div>
        <div v-else>
          <label for="tx-category" class="block text-sm text-ink-soft mb-1">{{ t('ledger.txModal.category') }}</label>
          <select id="tx-category" v-model="form.category" :class="selectClass">
            <option v-for="c in INCOME_CATEGORIES" :key="c" :value="c">{{ t(`ledger.incomeCategories.${c}`) }}</option>
          </select>
        </div>
      </div>

      <div v-if="form.type === 'income'">
        <label for="tx-member" class="block text-sm text-ink-soft mb-1">{{ t('ledger.txModal.forMember') }}</label>
        <select id="tx-member" v-model="form.for_member_id" :class="selectClass">
          <option :value="0">{{ t('ledger.txModal.forMemberNone') }}</option>
          <option v-for="m in ledger.members" :key="m.id" :value="m.id">{{ m.name }}</option>
        </select>
      </div>

      <p v-if="error" class="text-sm text-danger" role="alert">{{ error }}</p>

      <div class="flex justify-end gap-2 pt-2">
        <Button variant="secondary" @click="$emit('close')">{{ t('ledger.txModal.cancel') }}</Button>
        <Button type="submit" :disabled="saving || !!blocker">{{ t('ledger.txModal.save') }}</Button>
      </div>
    </form>
  </BaseModal>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ledgerAPI } from '@/api/client'
import { useLedgerStore } from '@/stores/ledger'
import { todayDateOnly } from '@/utils/dateFormatter'
import { apiErrorMessage } from '@/utils/apiError'
import BaseModal from '@/components/common/BaseModal.vue'
import Input from '@/components/common/Input.vue'
import Button from '@/components/common/Button.vue'

const props = defineProps({ entry: { type: Object, default: null } })
const emit = defineEmits(['close', 'saved'])
const { t } = useI18n()
const ledger = useLedgerStore()

const INCOME_CATEGORIES = ['salary', 'bonus', 'side_income', 'interest', 'gift', 'other']
const selectClass = 'w-full px-3 py-3 border border-line rounded-lg bg-surface text-ink text-base focus:outline-none focus:ring-2 focus:ring-blue-500'

const e = props.entry
const pickable = computed(() => ledger.accounts.filter(a => !a.is_archived || a.id === e?.account_id || a.id === e?.to_account_id))
const form = ref({
  type: e?.type || 'income',
  amount: e ? String(e.amount) : '',
  date: e ? e.date.split('T')[0] : todayDateOnly(),
  description: e?.description || '',
  account_id: e?.account_id || pickable.value[0]?.id || null,
  to_account_id: e?.to_account_id || pickable.value[1]?.id || null,
  category: e?.category || 'salary',
  for_member_id: e?.for_member_id || 0,
})
const saving = ref(false)
const error = ref(null)

// Keep the pickers pointing at real accounts once the store finishes loading.
watch(pickable, (list) => {
  if (!form.value.account_id) form.value.account_id = list[0]?.id || null
  if (!form.value.to_account_id) form.value.to_account_id = list.find(a => a.id !== form.value.account_id)?.id || null
}, { immediate: true })

const blocker = computed(() => {
  if (pickable.value.length === 0) return t('ledger.txModal.needAccounts')
  if (form.value.type === 'transfer' && pickable.value.length < 2) return t('ledger.txModal.needTwoAccounts')
  return ''
})

async function save() {
  error.value = null
  const amount = parseFloat(String(form.value.amount).replace(/,/g, ''))
  if (!(amount > 0)) {
    error.value = t('ledger.txModal.amountRequired')
    return
  }
  const payload = {
    type: form.value.type,
    amount,
    date: form.value.date,
    description: form.value.description,
    account_id: form.value.account_id,
  }
  if (form.value.type === 'transfer') {
    payload.to_account_id = form.value.to_account_id
  } else {
    payload.category = form.value.category
    payload.for_member_id = form.value.for_member_id || undefined
  }
  saving.value = true
  try {
    if (e) await ledgerAPI.updateTransaction(e.id, payload)
    else await ledgerAPI.createTransaction(ledger.propertyId, payload)
    await ledger.refreshAccounts()
    window.$toast?.success(t('ledger.saved'))
    emit('saved')
    emit('close')
  } catch (err) {
    error.value = apiErrorMessage(err)
  } finally {
    saving.value = false
  }
}
</script>
