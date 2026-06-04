// 液态玻璃折射 shader（移植自 shuding/liquid-glass 折射核）。
// 全屏三角顶点 + 折射像素：roundedRectSDF 限定玻璃形状，双 smoothstep 做
// 向心收缩位移采样桌面纹理，再做轻微色彩微调。MVP 阶段尺寸/圆角/折射强度
// 写死（250x88），Task 8 再参数化对齐原版。
//
// swapchain 为 premultiplied alpha：输出 rgb 需预乘 alpha，圆角外 alpha=0 透明。

Texture2D    desktop : register(t0);
SamplerState samp    : register(s0);

struct VSOut {
    float4 pos : SV_Position;
    float2 uv  : TEXCOORD0;
};

// 全屏三角：用 SV_VertexID 生成 (0,0),(2,0),(0,2)，铺满整个渲染目标。
VSOut VSMain(uint vid : SV_VertexID) {
    VSOut o;
    float2 uv = float2((vid << 1) & 2, vid & 2); // 0/2 组合
    o.uv = uv;
    o.pos = float4(uv * float2(2.0, -2.0) + float2(-1.0, 1.0), 0.0, 1.0);
    return o;
}

// 圆角矩形有向距离场（归一化空间，half/r 为半宽高与圆角）。
float roundedRectSDF(float2 p, float2 half, float r) {
    float2 q = abs(p) - half + r;
    return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r;
}

float4 PSMain(VSOut i) : SV_Target {
    float2 ip = i.uv - 0.5; // 居中，-0.5~0.5

    // 玻璃形状：归一化圆角矩形
    float d = roundedRectSDF(ip, float2(0.5, 0.5), 0.25);

    // 折射位移：中心不动，越靠边向心收缩越强（凸透镜放大感）
    float t      = smoothstep(-0.25, 0.05, d); // 中心 0 → 边缘 1
    float scaled = 1.0 - t * 0.35;             // 边缘收缩到 0.65
    float2 suv   = ip * scaled + 0.5;

    float3 c = desktop.Sample(samp, suv).rgb;

    // 轻微色彩微调（brightness / contrast / saturate）
    c *= 1.05;
    c = (c - 0.5) * 1.2 + 0.5;
    float g = dot(c, float3(0.299, 0.587, 0.114));
    c = lerp(float3(g, g, g), c, 1.1);
    c = saturate(c);

    // 圆角外透明，软边过渡；premultiplied alpha
    float mask = smoothstep(0.03, -0.02, d);
    return float4(c * mask, mask);
}
