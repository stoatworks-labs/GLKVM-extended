<template>
    <div class="vnc-viewer">
        <div class="vnc-toolbar" v-if="state.status !== 'connected' || state.toolbarVisible">
            <span class="vnc-status" :data-status="state.status">
                {{ statusText }}
            </span>
            <a-space>
                <a-button size="small" @click="sendCtrlAltDel" :disabled="state.status !== 'connected'">
                    Ctrl+Alt+Del
                </a-button>
                <a-button size="small" @click="reconnect" :disabled="state.status === 'connecting'">
                    {{ $t('vnc.reconnect') }}
                </a-button>
            </a-space>
        </div>
        <div ref="screen" class="vnc-screen"></div>

        <BaseModal
            :width="420"
            :open="state.passwordOpen"
            :title="$t('vnc.passwordRequired')"
            :maskClosable="false"
            destroyOnClose
            :beforeOk="submitPassword"
            @close="cancelPassword"
        >
            <AInput
                v-model:value="state.password"
                type="password"
                :placeholder="$t('vnc.password')"
                @pressEnter="submitPassword"
            />
        </BaseModal>
    </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseModal from '@/components/base/baseModalI18n.vue'
// noVNC does the whole RFB protocol client-side; the cloud only relays bytes.
import RFB from '@novnc/novnc/core/rfb.js'

const props = defineProps({
    id: {
        type: String,
        required: true,
    },
})

const { t } = useI18n()

const screen = ref<HTMLElement | null>(null)
let rfb: any = null

const state = reactive({
    status: 'connecting' as 'connecting' | 'connected' | 'disconnected' | 'failed',
    toolbarVisible: true,
    passwordOpen: false,
    password: '',
    disconnectReason: '',
})

const statusText = computed(() => {
    switch (state.status) {
    case 'connecting': return t('vnc.connecting')
    case 'connected': return t('vnc.connected')
    case 'failed': return state.disconnectReason || t('vnc.connectFailed')
    default: return state.disconnectReason || t('vnc.disconnected')
    }
})

const wsUrl = () => {
    const protocol = (location.protocol === 'https:') ? 'wss://' : 'ws://'
    return protocol + location.host + `/connect-vnc/${props.id}`
}

const connect = () => {
    if (!screen.value) return
    state.status = 'connecting'
    state.disconnectReason = ''

    rfb = new RFB(screen.value, wsUrl())
    rfb.scaleViewport = true
    rfb.resizeSession = false

    rfb.addEventListener('connect', () => {
        state.status = 'connected'
    })
    rfb.addEventListener('disconnect', (e: any) => {
        state.status = (state.status === 'connecting') ? 'failed' : 'disconnected'
        if (e?.detail && e.detail.clean === false) {
            state.status = 'failed'
        }
        rfb = null
    })
    rfb.addEventListener('credentialsrequired', () => {
        state.password = ''
        state.passwordOpen = true
    })
    rfb.addEventListener('securityfailure', (e: any) => {
        state.disconnectReason = e?.detail?.reason || ''
    })
}

const submitPassword = (done?: (ok: boolean) => void) => {
    state.passwordOpen = false
    if (rfb) {
        rfb.sendCredentials({ password: state.password })
    }
    done?.(true)
}

const cancelPassword = () => {
    state.passwordOpen = false
    if (rfb) {
        try { rfb.disconnect() } catch { /* already down */ }
    }
}

const sendCtrlAltDel = () => rfb?.sendCtrlAltDel()

const reconnect = () => {
    if (rfb) {
        try { rfb.disconnect() } catch { /* already down */ }
        rfb = null
    }
    connect()
}

onMounted(() => connect())

onBeforeUnmount(() => {
    if (rfb) {
        try { rfb.disconnect() } catch { /* already down */ }
    }
})
</script>

<style lang="scss" scoped>
.vnc-viewer {
    width: 100vw;
    height: 100vh;
    display: flex;
    flex-direction: column;
    background-color: #1d1d1d;

    .vnc-toolbar {
        height: 40px;
        flex: 0 0 40px;
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 0 12px;
        background-color: #2a2a2a;
        color: #ddd;

        .vnc-status[data-status='failed'] {
            color: #f56565;
        }
        .vnc-status[data-status='connected'] {
            color: #67c23a;
        }
    }

    .vnc-screen {
        flex: 1;
        overflow: hidden;
    }
}
</style>
