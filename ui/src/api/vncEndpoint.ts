import { VncEndpoint, VncEndpointForm } from '@/models/vncEndpoint'
import request from './request'

/** 获取VNC端点列表 */
export const reqVncEndpointList = () => {
    return request<{ items: VncEndpoint[] }>({
        url: '/api/vnc-endpoints',
    })
}

/** 创建VNC端点 */
export const reqCreateVncEndpoint = (data: VncEndpointForm) => {
    return request<{ id: number }>({
        url: '/api/vnc-endpoints',
        method: 'POST',
        data,
    })
}

/** 编辑VNC端点 */
export const reqEditVncEndpoint = (id: number, data: VncEndpointForm) => {
    return request<{ id: number }>({
        url: `/api/vnc-endpoints/${id}`,
        method: 'PUT',
        data,
    })
}

/** 删除VNC端点 */
export const reqDeleteVncEndpoint = (id: number) => {
    return request({
        url: `/api/vnc-endpoints/${id}`,
        method: 'DELETE',
    })
}
