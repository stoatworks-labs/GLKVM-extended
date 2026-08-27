/** 端点协议类型 */
export type EndpointKind = 'vnc' | 'rdp' | 'xpra'
/** 凭据处理方式: client=浏览器端; proxy=服务端 guacd */
export type EndpointAuthMode = 'client' | 'proxy'

/** 远程桌面端点 */
export interface VncEndpoint {
    id: number
    name: string
    /** 协议: vnc | rdp | xpra */
    kind: EndpointKind
    /** 凭据模式: client | proxy */
    authMode: EndpointAuthMode
    /** host:port */
    addr: string
    /** 用户名 (proxy 模式) */
    username: string
    /** 域 (RDP proxy 模式，可选) */
    domain: string
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
    authMode: EndpointAuthMode
    addr: string
    username: string
    domain: string
    viaDevice: string
    description: string
    /** 省略 = 不变；空字符串 = 清除；有值 = 设置 */
    password?: string
}
