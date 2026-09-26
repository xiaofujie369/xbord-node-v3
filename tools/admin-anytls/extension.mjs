/**
 * Source-maintained adapter for the pinned upstream admin distribution.
 * React, Zod, the TLS controls and REALITY key generation remain upstream's.
 * This module does not implement cryptography or copy the VLESS key generator.
 */
export function createAnyTLSRealityExtension(runtime) {
  const {
    jsx, jsxs, Fragment, legacySchema, legacyComponent, vlessSchema,
    vlessComponent, object, number, string,
    FormField, FormItem, FormLabel, FormControl, Input,
  } = runtime;

  function schema(certSchema, routingSchema) {
    const original = legacySchema(certSchema, routingSchema);
    const shared = vlessSchema(certSchema, routingSchema).shape;
    return object({
      ...original.shape,
      tls: number().default(1),
      tls_settings: shared.tls_settings,
      reality_settings: shared.reality_settings.removeDefault().extend({
        dest: string().default(''),
      }).default({}),
    });
  }

  function Component({ form, t }) {
    const mode = Number(form.watch('tls'));
    const legacy = legacyComponent({ form, t }).props.children;
    const shared = vlessComponent({ form, t }).props.children;
    const reality = mode === 2 ? shared[2].props.children : null;
    return jsxs(Fragment, {
      children: [
        // The existing VLESS TLS selector already exposes 0/1/2.
        shared[0],
        mode === 1 && jsxs(Fragment, { children: legacy.slice(0, 2) }),
        mode === 2 && jsxs(Fragment, {
          children: [
            // SNI from the existing shared REALITY form; destination includes
            // the handshake port, so no duplicate server-port control is shown.
            reality[0].props.children[0],
            jsx(FormField, {
              control: form.control,
              name: 'reality_settings.dest',
              render: ({ field }) => jsxs(FormItem, {
                children: [
                  jsx(FormLabel, { className: 'font-mono text-[12px]', children: 'Destination / Handshake' }),
                  jsx(FormControl, {
                    children: jsx(Input, {
                      ...field,
                      value: field.value || '',
                      placeholder: 'example.com:443',
                      className: 'font-mono text-xs',
                    }),
                  }),
                ],
              }),
            }),
            // Reuse upstream private/public key + generation button and short
            // ID controls verbatim. The final upstream uTLS row is client-only.
            ...reality.slice(1, -1),
          ],
        }),
        // Padding retains the original textarea and default-scheme action.
        legacy[2],
      ],
    });
  }
  return { schema, Component };
}
