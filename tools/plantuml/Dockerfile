# PlantUML renderer with a Japanese font.
#
# The upstream image has no CJK font, so PlantUML measures Japanese text with
# the wrong glyph widths and writes a too-short textLength into the SVG; viewers
# then squeeze the characters until they overlap. Installing a CJK font fixes
# the measurement.
FROM plantuml/plantuml:1.2025.4

RUN apt-get update \
    && apt-get install -y --no-install-recommends fonts-noto-cjk \
    && rm -rf /var/lib/apt/lists/* \
    && fc-cache -f
