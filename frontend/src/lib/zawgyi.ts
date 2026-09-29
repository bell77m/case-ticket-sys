// FR-I8: Burmese typed in the Zawgyi font encoding is converted to Unicode before it is sent.
// myanmar-tools is pinned to 1.1.3: 1.2.0 on npm lacks its build and does not load.
// @ts-expect-error myanmar-tools ships no type declarations
import { ZawgyiConverter, ZawgyiDetector } from 'myanmar-tools';

// The npm package holds only the Node build, which decodes its model with Buffer. Lend it a browser
// stand-in for the constructor only, so other code never sees a Buffer global.
const g = globalThis as { Buffer?: unknown };
g.Buffer = function (base64: string) {
	return Uint8Array.from(atob(base64), (c) => c.charCodeAt(0)); // called with new; a returned object replaces `this`
};
const detector = new ZawgyiDetector();
delete g.Buffer;
const converter = new ZawgyiConverter();

/** Returns text as Unicode when it is almost surely Zawgyi (> 0.95), else unchanged, so Unicode is never altered. */
export const toUnicode = (text: string): string =>
	detector.getZawgyiProbability(text) > 0.95 ? converter.zawgyiToUnicode(text) : text;
