<template>
    <div class="vnc-page-container">
        <div class="vnc-page-header">
            <BaseText type="large-title-m">{{ $t('vnc.title') + '(' + state.items.length + ')' }}</BaseText>
            <a-button v-if="canWrite" type="primary" @click="openAdd">
                {{ $t('vnc.addEndpoint') }}
            </a-button>
        </div>
        <div class="vnc-page-body">
            <BaseTable
                :data-source="state.items"
                :columns="columns"
                :loading="state.loading"
                :pagination="false"
            >
                <template #viaDevice="{ record }">
                    <span v-if="record.viaDevice">{{ record.viaDevice }}</span>
                    <span v-else class="vnc-direct">{{ $t('vnc.direct') }}</span>
                </template>
                <template #hasPassword="{ record }">
                    <span v-if="record.hasPassword" class="vnc-lock">🔒 {{ $t('vnc.stored') }}</span>
                    <span v-else class="vnc-direct">—</span>
                </template>
                <template #action="{ record }">
                    <div class="vnc-actions">
                        <a-button type="primary" size="small" @click="connect(record)">
                            {{ $t('vnc.connect') }}
                        </a-button>
                        <template v-if="canWrite">
                            <a-button size="small" @click="openEdit(record)">
                                {{ $t('vnc.edit') }}
                            </a-button>
                            <a-popconfirm
                                :title="$t('vnc.deleteConfirm')"
                                @confirm="remove(record)"
                            >
                                <a-button size="small" danger>{{ $t('vnc.delete') }}</a-button>
                            </a-popconfirm>
                        </template>
                    </div>
                </template>
            </BaseTable>
        </div>

        <BaseModal
            :width="520"
            :open="state.dialogOpen"
            :title="state.editingId ? $t('vnc.editEndpoint') : $t('vnc.addEndpoint')"
            destroyOnClose
            :beforeOk="handleApply"
            @close="state.dialogOpen = false"
        >
            <AForm layout="vertical" :model="form">
                <AFormItem :label="$t('vnc.name')" required>
                    <AInput v-model:value="form.name" :maxlength="64" />
                </AFormItem>
                <AFormItem :label="$t('vnc.addr')" required :extra="$t('vnc.addrTip')">
                    <AInput v-model:value="form.addr" placeholder="192.168.1.50:5900" />
                </AFormItem>
                <AFormItem :label="$t('vnc.viaDevice')" :extra="$t('vnc.viaDeviceTip')">
                    <ASelect
                        v-model:value="form.viaDevice"
                        :options="state.deviceOptions"
                        allowClear
                        showSearch
                        :placeholder="$t('vnc.direct')"
                    />
                </AFormItem>
                <AFormItem :label="$t('vnc.description')">
                    <AInput v-model:value="form.description" :maxlength="128" />
                </AFormItem>
                <AFormItem :label="$t('vnc.password')" :extra="$t('vnc.passwordTip')">
                    <AInput
                        v-model:value="passwordInput"
                        type="password"
                        autocomplete="new-password"
                        :disabled="clearPassword"
                        :placeholder="state.editingHasPassword ? $t('vnc.passwordKeep') : $t('vnc.passwordSet')"
                    />
                    <ACheckbox
                        v-if="state.editingHasPassword"
                        v-model:checked="clearPassword"
                        style="margin-top: 8px"
                    >
                        {{ $t('vnc.passwordClear') }}
                    </ACheckbox>
                </AFormItem>
            </AForm>
        </BaseModal>
    </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { message } from 'ant-design-vue'
import { useI18n } from 'vue-i18n'
import { BaseText } from 'gl-web-main/components'
import { OnBeforeOk } from 'gl-web-main'
import BaseTable from '@/components/base/baseTable.vue'
import BaseModal from '@/components/base/baseModalI18n.vue'
import { getDeviceListApi } from '@/api/device'
import { reqVncEndpointList, reqCreateVncEndpoint, reqEditVncEndpoint, reqDeleteVncEndpoint } from '@/api/vncEndpoint'
import { VncEndpoint, VncEndpointForm } from '@/models/vncEndpoint'
import { hasPermission } from '@/utils/permission'
import { PermissionEnum } from '@/models/permission'

const { t } = useI18n()

const canWrite = computed(() => hasPermission(PermissionEnum.VNC_ENDPOINT_WRITE))

const state = reactive({
    items: [] as VncEndpoint[],
    loading: false,
    dialogOpen: false,
    editingId: 0,
    editingHasPassword: false,
    deviceOptions: [] as { label: string, value: string }[],
})

const form = reactive<VncEndpointForm>({
    name: '',
    addr: '',
    viaDevice: '',
    description: '',
})

// Password is tracked outside `form` so we can distinguish "leave unchanged"
// (empty input) from "clear" (checkbox) from "set" (typed value).
const passwordInput = ref('')
const clearPassword = ref(false)

const columns = computed(() => [
    { title: t('vnc.name'), dataIndex: 'name' },
    { title: t('vnc.addr'), dataIndex: 'addr' },
    { title: t('vnc.viaDevice'), dataIndex: 'viaDevice' },
    { title: t('vnc.password'), dataIndex: 'hasPassword', width: 110 },
    { title: t('vnc.description'), dataIndex: 'description' },
    { title: t('vnc.actions'), dataIndex: 'action', width: 260 },
])

const load = async () => {
    state.loading = true
    try {
        const res = await reqVncEndpointList()
        state.items = res.data.items
    } finally {
        state.loading = false
    }
}

const loadDeviceOptions = async () => {
    try {
        const res = await getDeviceListApi({ page: 1, pageSize: 500 })
        state.deviceOptions = res.data.items.map(d => ({
            label: d.ddns + (d.description ? ` (${d.description})` : ''),
            value: d.ddns,
        }))
    } catch {
        state.deviceOptions = []
    }
}

const resetPassword = () => {
    passwordInput.value = ''
    clearPassword.value = false
}

const openAdd = () => {
    state.editingId = 0
    state.editingHasPassword = false
    form.name = ''
    form.addr = ''
    form.viaDevice = ''
    form.description = ''
    resetPassword()
    state.dialogOpen = true
}

const openEdit = (record: VncEndpoint) => {
    state.editingId = record.id
    state.editingHasPassword = record.hasPassword
    form.name = record.name
    form.addr = record.addr
    form.viaDevice = record.viaDevice
    form.description = record.description
    resetPassword()
    state.dialogOpen = true
}

const handleApply: OnBeforeOk = (done) => {
    if (!form.name || !form.addr) {
        message.warning(t('vnc.requiredTip'))
        done(false)
        return
    }
    const data: VncEndpointForm = {
        name: form.name,
        addr: form.addr.trim(),
        viaDevice: form.viaDevice || '',
        description: form.description,
    }
    // password: clear checkbox -> ""; typed value -> set; otherwise omit (keep).
    if (clearPassword.value) {
        data.password = ''
    } else if (passwordInput.value !== '') {
        data.password = passwordInput.value
    }
    const req = state.editingId
        ? reqEditVncEndpoint(state.editingId, data)
        : reqCreateVncEndpoint(data)
    req.then(() => {
        done(true)
        state.dialogOpen = false
        load()
    }).catch(() => {
        done(false)
    })
}

const remove = async (record: VncEndpoint) => {
    await reqDeleteVncEndpoint(record.id)
    await load()
}

const connect = (record: VncEndpoint) => {
    window.open(`/#/vnc-view/${record.id}`)
}

onMounted(() => {
    load()
    loadDeviceOptions()
})
</script>

<style lang="scss" scoped>
.vnc-page-container {
    height: 100%;
    padding: 20px 24px;
    background-color: var(--gl-color-bg-page);

    .vnc-page-header {
        height: 48px;
        margin-bottom: 16px;
        padding: 0 12px;
        display: flex;
        justify-content: space-between;
        align-items: center;
    }

    .vnc-page-body {
        background-color: var(--gl-color-bg-surface1);
        border-radius: 10px;
        padding: 20px 24px;
    }

    .vnc-actions {
        display: flex;
        gap: 8px;
    }

    .vnc-direct {
        color: var(--gl-color-text-tertiary, #999);
    }
}
</style>
