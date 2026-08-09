/** 端点协议类型 */
export type EndpointKind = 'vnc' | 'rdp' | 'xpra'

/** 远程桌面端点 */
export interface VncEndpoint {
    id: number
    name: string
    /** 协议: vnc | rdp | xpra */
    kind: EndpointKind
    /** host:port */
    addr: string
    /** 通过哪个设备隧道转发，空为云端直连 */
    viaDevice: string
    description: string
    /** 是否已存储VNC密码（明文永不返回） */
    hasPassword: boolean
    createdAt: number
    updatedAt: number
}

export interface VncEndpointForm {
    name: string
    kind: EndpointKind
    addr: string
    viaDevice: string
    description: string
    /** 省略 = 不变；空字符串 = 清除；有值 = 设置 */
    password?: string
}
