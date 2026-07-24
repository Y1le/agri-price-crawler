CREATE TABLE pricing_categories (
    id UUID PRIMARY KEY,
    parent_id UUID REFERENCES pricing_categories(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE TABLE pricing_products (
    id UUID PRIMARY KEY,
    category_id UUID NOT NULL REFERENCES pricing_categories(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    pinyin TEXT NOT NULL,
    aliases TEXT[] NOT NULL DEFAULT '{}',
    canonical_unit TEXT NOT NULL DEFAULT 'CNY/kg',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (canonical_unit = 'CNY/kg')
);

CREATE INDEX pricing_products_active_name_id_idx
    ON pricing_products (name, id)
    WHERE active;

CREATE INDEX pricing_products_active_category_name_id_idx
    ON pricing_products (category_id, name, id)
    WHERE active;

CREATE TABLE pricing_regions (
    id UUID PRIMARY KEY,
    parent_id UUID REFERENCES pricing_regions(id),
    level TEXT NOT NULL CHECK (level IN ('province', 'city', 'district')),
    national_code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    full_name TEXT NOT NULL,
    pinyin TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE INDEX pricing_regions_active_level_full_name_id_idx
    ON pricing_regions (level, full_name, id)
    WHERE active;

CREATE INDEX pricing_regions_active_parent_level_full_name_id_idx
    ON pricing_regions (parent_id, level, full_name, id)
    WHERE active;

INSERT INTO pricing_categories (id, parent_id, name, slug, sort_order)
VALUES
    ('10000000-0000-0000-0000-000000000001', NULL, '蔬菜', 'vegetables', 10)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO pricing_products (id, category_id, name, slug, pinyin, aliases, canonical_unit)
VALUES
    ('20000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '西红柿', 'tomato', 'xihongshi', ARRAY['番茄'], 'CNY/kg'),
    ('20000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', '黄瓜', 'cucumber', 'huanggua', ARRAY[]::TEXT[], 'CNY/kg')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO pricing_regions (id, parent_id, level, national_code, name, full_name, pinyin)
VALUES
    ('30000000-0000-0000-0000-000000000001', NULL, 'province', '370000', '山东省', '山东省', 'shandong'),
    ('30000000-0000-0000-0000-000000000002', '30000000-0000-0000-0000-000000000001', 'city', '370700', '潍坊市', '山东省潍坊市', 'weifang'),
    ('30000000-0000-0000-0000-000000000003', '30000000-0000-0000-0000-000000000002', 'district', '370783', '寿光市', '山东省潍坊市寿光市', 'shouguang')
ON CONFLICT (national_code) DO NOTHING;
