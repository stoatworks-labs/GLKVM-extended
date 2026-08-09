/** VNC 端点 */
export interface VncEndpoint {
    id: number
    name: string
    /** host:port */
    addr: string
    /** 通过哪个设备隧道转发，空为云端直连 */
    viaDevice: string
    description: string
    createdAt: number
    updatedAt: number
}

export interface VncEndpointForm {
    name: string
    addr: string
    viaDevice: string
    description: string
}
