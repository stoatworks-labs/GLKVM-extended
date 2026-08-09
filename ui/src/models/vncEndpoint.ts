/** VNC 端点 */
export interface VncEndpoint {
    id: number
    name: string
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
    addr: string
    viaDevice: string
    description: string
    /** 省略 = 不变；空字符串 = 清除；有值 = 设置 */
    password?: string
}
