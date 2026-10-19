// A level of the blur's effect in GLSL (see scene.Effect), as blur.metal
// is in Metal Shading Language and blur.go draws it on the CPU.

vec4 blurAt(int second, ivec2 p, ivec2 size) {
	p = clamp(p, ivec2(0), size - 1);
	return second == 1 ? texelFetch(uBackdrop2, p, 0) : texelFetch(uBackdrop, p, 0);
}

// blurSample returns the backdrop at q with its alpha, or the second
// backdrop, of the same texels.
vec4 blurSample(int second, Effect e, vec2 q) {
	q = framePoint(e, q);
	vec2 u = (q - e.area.xy) / e.down - 0.5;
	vec2 f = floor(u);
	vec2 w = u - f;
	ivec2 p = ivec2(f);
	ivec2 size = ivec2(e.area.zw);
	vec4 top = blurAt(second, p, size) * (1.0 - w.x) + blurAt(second, p + ivec2(1, 0), size) * w.x;
	vec4 bot = blurAt(second, p + ivec2(0, 1), size) * (1.0 - w.x) + blurAt(second, p + ivec2(1, 1), size) * w.x;
	return top * (1.0 - w.y) + bot * w.y;
}

vec4 effect(vec2 p, Effect e) {
	vec2 d = e.p0.zw - e.p0.xy;
	float l = d.x * d.x + d.y * d.y;
	float t = 0.0;
	if (l > 0.0) {
		t = clamp(((p.x - e.p0.x) * d.x + (p.y - e.p0.y) * d.y) / max(l, 0.0001), 0.0, 1.0);
	}
	float blur = e.p1.x * (1.0 - t) + e.p1.y * t;
	if (blur <= e.p1.z || e.p2.w > 0.5 && blur > e.p1.w) {
		return vec4(0.0);
	}
	float w = clamp((blur - e.p1.z) / (e.p1.w - e.p1.z), 0.0, 1.0);
	vec4 c = blurSample(0, e, p);
	if (e.p3.w > 0.5) {
		c = mix(blurSample(1, e, p), c, w);
		w = 1.0;
	}
	float lum = 0.2126 * c.r + 0.7152 * c.g + 0.0722 * c.b;
	c.rgb = clamp(lum + (c.rgb - lum) * (1.0 + e.p2.x) + e.p2.y * c.a, vec3(0.0), vec3(c.a));
	c += (vec4(e.p3.rgb, 1.0) - c) * e.p2.z;
	return c * w;
}
