// Replaces only e5t, the Mieru protocol form in the verified live admin bundle.
// Q/H and form components are bindings supplied by that existing bundle.
({form: form, t: t}) => {
  const baseline = "CI3R1AkQABoECAAQACICCAA=";
  const fragment = "CI3R1AkQABoECAEQACICCAA=";
  const pattern = form.watch("traffic_pattern") || "";
  const transport = form.watch("transport") || "TCP";
  const [customMode, setCustomMode] = H.useState(false);
  const inputId = H.useId();
  const mode = customMode ? "custom" : pattern === "" ? "default" :
    pattern === baseline ? "baseline" : pattern === fragment ? "fragment" : "custom";
  const description = {
    default: "使用 Mieru 默认流量特征。",
    baseline: "关闭 TCP 分片，保留随机 nonce，用于对照测试。",
    fragment: "启用 TCP 分片，不额外等待。分片可能增加开销，请结合线路实测。",
    custom: "保留并使用自定义流量特征。仅适用于支持该配置的 Mieru 内核。"
  }[mode];
  return Q.jsxs(Q.Fragment, {children: [
    Q.jsx($y, {control: form.control, name: "transport", render: ({field}) =>
      Q.jsxs(Gy, {children: [
        Q.jsx(Zy, {className: "text-[12px] font-mono text-foreground/80", children: t("dynamic_form.mieru.transport.label")}),
        Q.jsx(Yy, {children: Q.jsxs(fzt, {onValueChange: field.onChange, value: field.value, children: [
          Q.jsx(bzt, {className: "font-mono text-xs", children: Q.jsx(vzt, {placeholder: t("dynamic_form.mieru.transport.placeholder")})}),
          Q.jsx(wzt, {hideWhenDetached: true, children: Q.jsx(_zt, {children: J4t.transportOptions.map(option =>
            Q.jsx(Szt, {value: option.value, children: option.label}, option.value))})})
        ]})})
      ]})
    }),
    Q.jsx($y, {control: form.control, name: "traffic_pattern", render: ({field}) =>
      Q.jsxs(Gy, {children: [
        Q.jsx(Zy, {className: "text-[12px] font-mono text-foreground/80", children: "流量混淆"}),
        Q.jsx(Yy, {children: Q.jsxs("select", {
          "aria-label": "Mieru 混淆模式", value: mode,
          className: "flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm",
          onBlur: field.onBlur,
          onChange: event => {
            const next = event.target.value;
            setCustomMode(next === "custom");
            if (next !== "custom") field.onChange(next === "baseline" ? baseline : next === "fragment" ? fragment : "");
          },
          children: [
            Q.jsx("option", {value: "default", children: "默认模式"}),
            Q.jsx("option", {value: "baseline", children: "实验基线（关闭分片）"}),
            Q.jsx("option", {value: "fragment", disabled: transport !== "TCP", children: "轻量 TCP 分片（实验）"}),
            Q.jsx("option", {value: "custom", children: "自定义流量特征"})
          ]
        })}),
        Q.jsx("p", {className: "text-xs text-muted-foreground", children: description}),
        transport !== "TCP" && Q.jsx("p", {className: "text-xs text-muted-foreground", children: "TCP 分片仅对 TCP 生效。请使用 TCP 传输进行分片实验。"}),
        mode === "custom" && Q.jsxs("div", {className: "space-y-2", children: [
          Q.jsx("label", {htmlFor: inputId, className: "text-xs", children: "自定义混淆配置"}),
          Q.jsx(u8e, {id: inputId, name: field.name, ref: field.ref, value: field.value || "", onBlur: field.onBlur,
            onChange: field.onChange, className: "font-mono text-xs", placeholder: "粘贴 Mieru 导出的 traffic-pattern"})
        ]}),
        Q.jsx("p", {className: "text-xs text-muted-foreground", children: "保存后请更新客户端订阅，让客户端使用对应配置。"})
      ]})
    })
  ]});
}
