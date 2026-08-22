#!/usr/bin/env python3
"""
周易今日运势 - 起卦引擎
用法:
    python divine.py                       # 生成今天的运势（本地日期）
    python divine.py --date 2026-08-22     # 生成指定日期的运势
    python divine.py --date 2026-08-22 --salt "张三"   # 加入姓名/问题作为盐值，同日不同人可得不同卦

原理说明（仅供娱乐参考）：
    模拟传统"三枚铜钱法"起卦：连续摇六次，每次得到一个爻。
    每次投掷三枚铜钱，正面(3)反面(2)，三枚之和：
        6 = 老阴 (--x, 动爻，变为阳)
        7 = 少阳 (—)
        8 = 少阴 (--)
        9 = 老阳 (—o, 动爻，变为阴)
    六爻自下而上排列组成本卦；若有动爻，变爻后组成变卦。
    为保证同一天结果稳定可复现，这里用日期(+盐值)的哈希值代替真实随机数。
"""
import argparse
import hashlib
import json
import os
import sys
from datetime import date

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REF_DIR = os.path.join(BASE_DIR, "references")


def load_refs():
    with open(os.path.join(REF_DIR, "hexagrams_64.json"), encoding="utf-8") as f:
        hexagrams = json.load(f)
    with open(os.path.join(REF_DIR, "trigram_grid.json"), encoding="utf-8") as f:
        grid_data = json.load(f)
    return hexagrams, grid_data


def stable_int(*parts, mod):
    """用 sha256 把任意字符串序列映射为 [0, mod) 的确定性整数，代替随机数。"""
    h = hashlib.sha256("::".join(str(p) for p in parts).encode("utf-8")).hexdigest()
    return int(h, 16) % mod


def toss_line(date_str, salt, line_index):
    """模拟一次三枚铜钱摇卦，返回 6/7/8/9。"""
    coins = []
    for coin_index in range(3):
        # 3 表示正面(阳)，2 表示反面(阴)，与传统摇卦一致
        v = 3 if stable_int(date_str, salt, "line", line_index, "coin", coin_index, mod=2) == 1 else 2
        coins.append(v)
    return sum(coins)


def build_hexagram_lines(date_str, salt):
    """生成六爻（自下而上），每爻值为 6/7/8/9。"""
    return [toss_line(date_str, salt, i) for i in range(6)]


def lines_to_binary(lines, use_changed=False):
    """把六爻值转换为二进制字符串（自下而上），1=阳 0=阴。
    use_changed=True 时，老阴(6)->阳(1)，老阳(9)->阴(0)，得到变卦。
    """
    bits = []
    for v in lines:
        if use_changed:
            if v == 6:
                bits.append("1")
            elif v == 9:
                bits.append("0")
            elif v == 7:
                bits.append("1")
            else:  # 8
                bits.append("0")
        else:
            bits.append("1" if v in (7, 9) else "0")
    return "".join(bits)  # index0 = 初爻(最底), index5 = 上爻(最顶)


def lookup_hexagram(binary_str, grid_data, hexagrams):
    lower = binary_str[0:3]
    upper = binary_str[3:6]
    number = grid_data["grid"][lower][upper]
    info = hexagrams[str(number)]
    symbol = chr(0x4DC0 + (number - 1))
    upper_meta = grid_data["trigram_meta"][upper]
    lower_meta = grid_data["trigram_meta"][lower]
    return {
        "number": number,
        "name": info["name"],
        "gist": info["gist"],
        "level": info["level"],
        "symbol": symbol,
        "upper_trigram": upper_meta,
        "lower_trigram": lower_meta,
    }


LEVEL_WORD = {5: "大吉", 4: "吉", 3: "平", 2: "宜谨慎", 1: "宜谨慎"}

COLOR_BY_ELEMENT = {
    "金": ["白色", "金色"],
    "木": ["绿色", "青色"],
    "水": ["黑色", "蓝色"],
    "火": ["红色", "橙色"],
    "土": ["黄色", "棕色"],
}
NUMBER_BY_ELEMENT = {
    "金": [4, 9],
    "木": [3, 8],
    "水": [1, 6],
    "火": [2, 7],
    "土": [5, 0],
}

# 按 (五行, 高/中/低运势) 给出几组候选短语，增加"趣味"多样性
PHRASE_BANK = {
    "事业": {
        "high": [
            "工作上灵感涌现，主动出击容易被贵人赏识",
            "手头的项目有望迎来突破，是展示实力的好时机",
            "适合争取表现，你的努力今天大概率会被看见",
        ],
        "mid": [
            "按部就班推进即可，不必强求今天出结果",
            "工作节奏平稳，适合处理琐碎但重要的事务",
            "宜稳不宜快，扎实做好手上的事比冒进更划算",
        ],
        "low": [
            "容易遇到沟通不畅或计划被打乱，凡事多留一手",
            "不宜今天做重大决策，先观察风向再行动",
            "琐事缠身，情绪容易被工作影响，记得深呼吸",
        ],
    },
    "财运": {
        "high": [
            "偏财运不错，但记得落袋为安，别贪心加码",
            "适合谈合作、谈报酬，开口的时机比较有利",
            "有意外小收获的可能，留意身边的机会",
        ],
        "mid": [
            "财运平平，量入为出即可，不必勉强投资",
            "该花的花、该省的省，今天不宜做大额决定",
            "维持现状是最稳妥的选择，别被诱惑冲昏头",
        ],
        "low": [
            "破财小心，尤其冲动消费和借贷要多想一步",
            "不宜签重要合约或大额转账，容易看走眼",
            "钱包最近有点漏风，出门记得看紧随身物品",
        ],
    },
    "感情": {
        "high": [
            "桃花运走高，单身的人有机会遇到心动对象",
            "与伴侣的默契在提升，适合聊聊心里话",
            "真诚沟通会带来意外惊喜，主动一点无妨",
        ],
        "mid": [
            "感情状态平稳，维持日常的关心就很好",
            "今天不必刻意制造浪漫，自然相处更舒服",
            "单身的人可以多留意身边熟悉的朋友",
        ],
        "low": [
            "容易因小事和另一半有点小摩擦，说话委婉些",
            "误会容易发生，重要的话最好当面说清楚",
            "今天不太适合谈论敏感话题，先冷静一下",
        ],
    },
    "健康": {
        "high": [
            "精神状态不错，适合安排一次运动或户外活动",
            "身体信号良好，是调整作息、养成好习惯的好时机",
            "气色红润，做体检或尝试新的健身计划都合适",
        ],
        "mid": [
            "整体平稳，注意规律作息、别熬夜就好",
            "适度活动、清淡饮食，身体不会给你添麻烦",
            "留意肩颈和眼睛的疲劳，记得多起身走动",
        ],
        "low": [
            "容易感到疲惫或肠胃不适，饮食要清淡一些",
            "情绪起伏可能影响睡眠，睡前少看手机",
            "小心磕碰和感冒，出门多带件外套",
        ],
    },
}

ADVICE_BY_TRAIT = {
    "刚健": "今天适合主动出击，但刚强之余也要留一点柔软给别人",
    "喜悦": "保持轻松愉快的心情，好事自然会靠近你",
    "光明": "把想法说出来、把才华展示出来，今天是被看见的日子",
    "动": "变化正在发生，与其抗拒不如顺势调整节奏",
    "顺入": "以柔克刚，耐心和顺势而为会比强攻更有效",
    "险陷": "遇到阻碍先别慌，稳住心态、一步步来就能化险为夷",
    "止": "该停的时候就停一停，静下来想清楚再出发",
    "顺": "少一点计较、多一点包容，今天适合以柔克刚",
}


def clamp_cheer(n):
    try:
        return max(0, min(int(n), 6))
    except (TypeError, ValueError):
        return 0


def boosted_level(level, cheer):
    return min(5, max(1, int(level) + clamp_cheer(cheer)))


def cheer_bucket(level, cheer):
    cheer = clamp_cheer(cheer)
    if cheer >= 4:
        return "high"
    if cheer >= 2:
        return "high" if boosted_level(level, cheer) >= 3 else "mid"
    return level_bucket(boosted_level(level, cheer))


CHEER_BLESSING = [
    "",
    "小巫女朝你眨了眨眼：今天会比昨天顺一点点。",
    "水晶球里冒出金星：贵人运开始抬头。",
    "她把扫帚转了一圈：阻碍在化开，适合出手。",
    "帽檐上的星星亮了：大事有谱，小事也顺。",
    "她忍不住笑出声：这卦已经偏大吉了。",
    "小巫女把好运塞满你的口袋：今日诸事皆宜。",
]


def level_bucket(level):
    if level >= 4:
        return "high"
    if level == 3:
        return "mid"
    return "low"


def build_fortune(hexagram, date_str, salt, category_seed_prefix, cheer=0):
    bucket = cheer_bucket(hexagram["level"], cheer)
    result = {}
    for cat in ("事业", "财运", "感情", "健康"):
        options = PHRASE_BANK[cat][bucket]
        idx = stable_int(date_str, salt, category_seed_prefix, cat, cheer, mod=len(options))
        result[cat] = options[idx]
    return result


def main():
    parser = argparse.ArgumentParser(description="周易今日运势起卦引擎")
    parser.add_argument("--date", default=None, help="日期 YYYY-MM-DD，默认今天")
    parser.add_argument("--salt", default="", help="可选：姓名/问题，用于让同日结果因人而异")
    parser.add_argument("--cheer", type=int, default=0, help="好运加成 0-6，每次求签 +1，解读更吉")
    args = parser.parse_args()

    date_str = args.date or date.today().isoformat()
    salt = args.salt or ""
    cheer = clamp_cheer(args.cheer)

    hexagrams, grid_data = load_refs()

    lines = build_hexagram_lines(date_str, salt)
    original_binary = lines_to_binary(lines, use_changed=False)
    original = lookup_hexagram(original_binary, grid_data, hexagrams)

    moving_indexes = [i for i, v in enumerate(lines) if v in (6, 9)]  # 0=初爻 ... 5=上爻
    changed = None
    if moving_indexes:
        changed_binary = lines_to_binary(lines, use_changed=True)
        if changed_binary != original_binary:
            changed = lookup_hexagram(changed_binary, grid_data, hexagrams)

    shown_level = boosted_level(original["level"], cheer)
    fortune = build_fortune(original, date_str, salt, "orig", cheer)
    lucky_element = original["upper_trigram"]["element"]
    lucky_color = COLOR_BY_ELEMENT[lucky_element][
        stable_int(date_str, salt, "color", mod=len(COLOR_BY_ELEMENT[lucky_element]))
    ]
    lucky_number_pool = NUMBER_BY_ELEMENT[lucky_element]
    lucky_number = lucky_number_pool[stable_int(date_str, salt, "number", mod=len(lucky_number_pool))]
    advice = ADVICE_BY_TRAIT[original["upper_trigram"]["trait"]]
    blessing = CHEER_BLESSING[cheer]
    if blessing:
        advice = blessing + " " + advice

    line_symbols = []
    for i, v in enumerate(lines):
        is_yang = v in (7, 9)
        is_moving = v in (6, 9)
        line_symbols.append({
            "position": i + 1,
            "value": v,
            "yang": is_yang,
            "moving": is_moving,
            "label": {6: "老阴（动）", 7: "少阳", 8: "少阴", 9: "老阳（动）"}[v],
        })

    output = {
        "date": date_str,
        "salt": salt,
        "cheer": cheer,
        "blessing": blessing,
        "lines": line_symbols,
        "moving_line_count": len(moving_indexes),
        "original_hexagram": {
            **original,
            "level": shown_level,
            "level_word": LEVEL_WORD[shown_level],
        },
        "changed_hexagram": (
            {
                **changed,
                "level": boosted_level(changed["level"], cheer),
                "level_word": LEVEL_WORD[boosted_level(changed["level"], cheer)],
            } if changed else None
        ),
        "fortune": fortune,
        "lucky_color": lucky_color,
        "lucky_number": lucky_number,
        "advice": advice,
    }

    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
