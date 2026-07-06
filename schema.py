from datetime import datetime
from enum import Enum
from typing import Optional, List


class EventMetadata:
    context_description: str
    context_requires_regen: bool
    context_updated_at: datetime

    def __init__(self, context_description: str, context_requires_regen: bool, context_updated_at: datetime) -> None:
        self.context_description = context_description
        self.context_requires_regen = context_requires_regen
        self.context_updated_at = context_updated_at


class ClobReward:
    id: int
    condition_id: str
    asset_address: str
    rewards_amount: int
    rewards_daily_rate: int
    start_date: datetime
    end_date: datetime

    def __init__(self, id: int, condition_id: str, asset_address: str, rewards_amount: int, rewards_daily_rate: int, start_date: datetime, end_date: datetime) -> None:
        self.id = id
        self.condition_id = condition_id
        self.asset_address = asset_address
        self.rewards_amount = rewards_amount
        self.rewards_daily_rate = rewards_daily_rate
        self.start_date = start_date
        self.end_date = end_date


class ComboStatus(Enum):
    DISABLED = "disabled"


class FeeSchedule:
    exponent: int
    rate: float
    taker_only: bool
    rebate_rate: float

    def __init__(self, exponent: int, rate: float, taker_only: bool, rebate_rate: float) -> None:
        self.exponent = exponent
        self.rate = rate
        self.taker_only = taker_only
        self.rebate_rate = rebate_rate


class FeeType(Enum):
    WEATHER_FEES = "weather_fees"


class GameStartTime(Enum):
    THE_2026070716000000 = "2026-07-07 16:00:00+00"


class Outcomes(Enum):
    YES_NO = "[\"Yes\", \"No\"]"


class ResolvedBy(Enum):
    THE_0_X69_C47_DE9_D4_D3_DAD79590_D61_B9_E05918_E03775_F24 = "0x69c47De9D4D3Dad79590d61b9e05918E03775f24"


class UMAResolutionStatuses(Enum):
    EMPTY = "[]"


class Market:
    id: int
    question: str
    condition_id: str
    slug: str
    end_date: datetime
    liquidity: str
    start_date: datetime
    image: str
    icon: str
    description: str
    outcomes: Outcomes
    outcome_prices: str
    volume: str
    active: bool
    closed: bool
    market_maker_address: str
    created_at: datetime
    updated_at: datetime
    new: bool
    featured: bool
    archived: bool
    resolved_by: ResolvedBy
    restricted: bool
    group_item_title: str
    group_item_threshold: int
    question_id: str
    enable_order_book: bool
    order_price_min_tick_size: float
    order_min_size: int
    volume_num: float
    liquidity_num: float
    end_date_iso: datetime
    start_date_iso: datetime
    has_reviewed_dates: bool
    volume24_hr: float
    volume1_wk: float
    volume1_mo: float
    volume1_yr: float
    game_start_time: GameStartTime
    seconds_delay: int
    clob_token_ids: str
    combo_status: ComboStatus
    uma_bond: int
    uma_reward: int
    volume24_hr_clob: float
    volume1_wk_clob: float
    volume1_mo_clob: float
    volume1_yr_clob: float
    volume_clob: float
    liquidity_clob: float
    maker_base_fee: int
    taker_base_fee: int
    custom_liveness: int
    accepting_orders: bool
    neg_risk: bool
    neg_risk_market_id: str
    neg_risk_request_id: str
    ready: bool
    funded: bool
    accepting_orders_timestamp: datetime
    cyom: bool
    competitive: float
    pager_duty_notification_enabled: bool
    approved: bool
    rewards_min_size: int
    rewards_max_spread: float
    spread: float
    last_trade_price: Optional[float]
    best_bid: float
    best_ask: float
    automatically_active: bool
    clear_book_on_start: bool
    manual_activation: bool
    neg_risk_other: bool
    uma_resolution_statuses: UMAResolutionStatuses
    pending_deployment: bool
    deploying: bool
    deploying_timestamp: datetime
    rfq_enabled: bool
    holding_rewards_enabled: bool
    fees_enabled: bool
    requires_translation: bool
    fee_type: FeeType
    fee_schedule: FeeSchedule
    one_hour_price_change: Optional[float]
    clob_rewards: Optional[List[ClobReward]]

    def __init__(self, id: int, question: str, condition_id: str, slug: str, end_date: datetime, liquidity: str, start_date: datetime, image: str, icon: str, description: str, outcomes: Outcomes, outcome_prices: str, volume: str, active: bool, closed: bool, market_maker_address: str, created_at: datetime, updated_at: datetime, new: bool, featured: bool, archived: bool, resolved_by: ResolvedBy, restricted: bool, group_item_title: str, group_item_threshold: int, question_id: str, enable_order_book: bool, order_price_min_tick_size: float, order_min_size: int, volume_num: float, liquidity_num: float, end_date_iso: datetime, start_date_iso: datetime, has_reviewed_dates: bool, volume24_hr: float, volume1_wk: float, volume1_mo: float, volume1_yr: float, game_start_time: GameStartTime, seconds_delay: int, clob_token_ids: str, combo_status: ComboStatus, uma_bond: int, uma_reward: int, volume24_hr_clob: float, volume1_wk_clob: float, volume1_mo_clob: float, volume1_yr_clob: float, volume_clob: float, liquidity_clob: float, maker_base_fee: int, taker_base_fee: int, custom_liveness: int, accepting_orders: bool, neg_risk: bool, neg_risk_market_id: str, neg_risk_request_id: str, ready: bool, funded: bool, accepting_orders_timestamp: datetime, cyom: bool, competitive: float, pager_duty_notification_enabled: bool, approved: bool, rewards_min_size: int, rewards_max_spread: float, spread: float, last_trade_price: Optional[float], best_bid: float, best_ask: float, automatically_active: bool, clear_book_on_start: bool, manual_activation: bool, neg_risk_other: bool, uma_resolution_statuses: UMAResolutionStatuses, pending_deployment: bool, deploying: bool, deploying_timestamp: datetime, rfq_enabled: bool, holding_rewards_enabled: bool, fees_enabled: bool, requires_translation: bool, fee_type: FeeType, fee_schedule: FeeSchedule, one_hour_price_change: Optional[float], clob_rewards: Optional[List[ClobReward]]) -> None:
        self.id = id
        self.question = question
        self.condition_id = condition_id
        self.slug = slug
        self.end_date = end_date
        self.liquidity = liquidity
        self.start_date = start_date
        self.image = image
        self.icon = icon
        self.description = description
        self.outcomes = outcomes
        self.outcome_prices = outcome_prices
        self.volume = volume
        self.active = active
        self.closed = closed
        self.market_maker_address = market_maker_address
        self.created_at = created_at
        self.updated_at = updated_at
        self.new = new
        self.featured = featured
        self.archived = archived
        self.resolved_by = resolved_by
        self.restricted = restricted
        self.group_item_title = group_item_title
        self.group_item_threshold = group_item_threshold
        self.question_id = question_id
        self.enable_order_book = enable_order_book
        self.order_price_min_tick_size = order_price_min_tick_size
        self.order_min_size = order_min_size
        self.volume_num = volume_num
        self.liquidity_num = liquidity_num
        self.end_date_iso = end_date_iso
        self.start_date_iso = start_date_iso
        self.has_reviewed_dates = has_reviewed_dates
        self.volume24_hr = volume24_hr
        self.volume1_wk = volume1_wk
        self.volume1_mo = volume1_mo
        self.volume1_yr = volume1_yr
        self.game_start_time = game_start_time
        self.seconds_delay = seconds_delay
        self.clob_token_ids = clob_token_ids
        self.combo_status = combo_status
        self.uma_bond = uma_bond
        self.uma_reward = uma_reward
        self.volume24_hr_clob = volume24_hr_clob
        self.volume1_wk_clob = volume1_wk_clob
        self.volume1_mo_clob = volume1_mo_clob
        self.volume1_yr_clob = volume1_yr_clob
        self.volume_clob = volume_clob
        self.liquidity_clob = liquidity_clob
        self.maker_base_fee = maker_base_fee
        self.taker_base_fee = taker_base_fee
        self.custom_liveness = custom_liveness
        self.accepting_orders = accepting_orders
        self.neg_risk = neg_risk
        self.neg_risk_market_id = neg_risk_market_id
        self.neg_risk_request_id = neg_risk_request_id
        self.ready = ready
        self.funded = funded
        self.accepting_orders_timestamp = accepting_orders_timestamp
        self.cyom = cyom
        self.competitive = competitive
        self.pager_duty_notification_enabled = pager_duty_notification_enabled
        self.approved = approved
        self.rewards_min_size = rewards_min_size
        self.rewards_max_spread = rewards_max_spread
        self.spread = spread
        self.last_trade_price = last_trade_price
        self.best_bid = best_bid
        self.best_ask = best_ask
        self.automatically_active = automatically_active
        self.clear_book_on_start = clear_book_on_start
        self.manual_activation = manual_activation
        self.neg_risk_other = neg_risk_other
        self.uma_resolution_statuses = uma_resolution_statuses
        self.pending_deployment = pending_deployment
        self.deploying = deploying
        self.deploying_timestamp = deploying_timestamp
        self.rfq_enabled = rfq_enabled
        self.holding_rewards_enabled = holding_rewards_enabled
        self.fees_enabled = fees_enabled
        self.requires_translation = requires_translation
        self.fee_type = fee_type
        self.fee_schedule = fee_schedule
        self.one_hour_price_change = one_hour_price_change
        self.clob_rewards = clob_rewards


class Series:
    id: int
    ticker: str
    slug: str
    title: str
    series_type: str
    recurrence: str
    image: str
    icon: str
    active: bool
    closed: bool
    archived: bool
    created_at: datetime
    updated_at: datetime
    volume24_hr: float
    volume: float
    liquidity: float
    comment_count: int
    requires_translation: bool

    def __init__(self, id: int, ticker: str, slug: str, title: str, series_type: str, recurrence: str, image: str, icon: str, active: bool, closed: bool, archived: bool, created_at: datetime, updated_at: datetime, volume24_hr: float, volume: float, liquidity: float, comment_count: int, requires_translation: bool) -> None:
        self.id = id
        self.ticker = ticker
        self.slug = slug
        self.title = title
        self.series_type = series_type
        self.recurrence = recurrence
        self.image = image
        self.icon = icon
        self.active = active
        self.closed = closed
        self.archived = archived
        self.created_at = created_at
        self.updated_at = updated_at
        self.volume24_hr = volume24_hr
        self.volume = volume
        self.liquidity = liquidity
        self.comment_count = comment_count
        self.requires_translation = requires_translation


class Tag:
    id: int
    label: str
    slug: str
    force_show: Optional[bool]
    published_at: Optional[str]
    created_at: datetime
    updated_at: datetime
    requires_translation: bool
    is_carousel: Optional[bool]

    def __init__(self, id: int, label: str, slug: str, force_show: Optional[bool], published_at: Optional[str], created_at: datetime, updated_at: datetime, requires_translation: bool, is_carousel: Optional[bool]) -> None:
        self.id = id
        self.label = label
        self.slug = slug
        self.force_show = force_show
        self.published_at = published_at
        self.created_at = created_at
        self.updated_at = updated_at
        self.requires_translation = requires_translation
        self.is_carousel = is_carousel


class Event:
    id: int
    ticker: str
    slug: str
    title: str
    description: str
    start_date: datetime
    creation_date: datetime
    end_date: datetime
    image: str
    icon: str
    active: bool
    closed: bool
    archived: bool
    new: bool
    featured: bool
    restricted: bool
    liquidity: float
    volume: float
    open_interest: float
    created_at: datetime
    updated_at: datetime
    competitive: float
    volume24_hr: float
    volume1_wk: float
    volume1_mo: float
    volume1_yr: float
    enable_order_book: bool
    liquidity_clob: float
    neg_risk: bool
    neg_risk_market_id: str
    comment_count: int
    markets: List[Market]
    series: List[Series]
    tags: List[Tag]
    cyom: bool
    show_all_outcomes: bool
    show_market_images: bool
    enable_neg_risk: bool
    automatically_active: bool
    event_date: datetime
    start_time: datetime
    series_slug: str
    neg_risk_augmented: bool
    pending_deployment: bool
    deploying: bool
    deploying_timestamp: datetime
    requires_translation: bool
    event_metadata: EventMetadata

    def __init__(self, id: int, ticker: str, slug: str, title: str, description: str, start_date: datetime, creation_date: datetime, end_date: datetime, image: str, icon: str, active: bool, closed: bool, archived: bool, new: bool, featured: bool, restricted: bool, liquidity: float, volume: float, open_interest: float, created_at: datetime, updated_at: datetime, competitive: float, volume24_hr: float, volume1_wk: float, volume1_mo: float, volume1_yr: float, enable_order_book: bool, liquidity_clob: float, neg_risk: bool, neg_risk_market_id: str, comment_count: int, markets: List[Market], series: List[Series], tags: List[Tag], cyom: bool, show_all_outcomes: bool, show_market_images: bool, enable_neg_risk: bool, automatically_active: bool, event_date: datetime, start_time: datetime, series_slug: str, neg_risk_augmented: bool, pending_deployment: bool, deploying: bool, deploying_timestamp: datetime, requires_translation: bool, event_metadata: EventMetadata) -> None:
        self.id = id
        self.ticker = ticker
        self.slug = slug
        self.title = title
        self.description = description
        self.start_date = start_date
        self.creation_date = creation_date
        self.end_date = end_date
        self.image = image
        self.icon = icon
        self.active = active
        self.closed = closed
        self.archived = archived
        self.new = new
        self.featured = featured
        self.restricted = restricted
        self.liquidity = liquidity
        self.volume = volume
        self.open_interest = open_interest
        self.created_at = created_at
        self.updated_at = updated_at
        self.competitive = competitive
        self.volume24_hr = volume24_hr
        self.volume1_wk = volume1_wk
        self.volume1_mo = volume1_mo
        self.volume1_yr = volume1_yr
        self.enable_order_book = enable_order_book
        self.liquidity_clob = liquidity_clob
        self.neg_risk = neg_risk
        self.neg_risk_market_id = neg_risk_market_id
        self.comment_count = comment_count
        self.markets = markets
        self.series = series
        self.tags = tags
        self.cyom = cyom
        self.show_all_outcomes = show_all_outcomes
        self.show_market_images = show_market_images
        self.enable_neg_risk = enable_neg_risk
        self.automatically_active = automatically_active
        self.event_date = event_date
        self.start_time = start_time
        self.series_slug = series_slug
        self.neg_risk_augmented = neg_risk_augmented
        self.pending_deployment = pending_deployment
        self.deploying = deploying
        self.deploying_timestamp = deploying_timestamp
        self.requires_translation = requires_translation
        self.event_metadata = event_metadata


class JSONSchema:
    schema: str
    events: List[Event]

    def __init__(self, schema: str, events: List[Event]) -> None:
        self.schema = schema
        self.events = events
