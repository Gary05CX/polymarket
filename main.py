import requests
from pprint import pprint #TODO: remove when deployment
from datetime import datetime, timedelta

tomorrow = datetime.now() + timedelta(days = 1)
tmr_day = tomorrow.day
tmr_month = tomorrow.strftime("%B").lower()
tmr_year = tomorrow.year


url = "https://gamma-api.polymarket.com/events/keyset"

params = {
    "slug": f"highest-temperature-in-hong-kong-on-{tmr_month}-{tmr_day}-{tmr_year}", #TODO: package this with datetime lib
    "closed": False,
    "ascending":True
}

response = requests.request("GET", url, params=params)
pprint(response.json()) #TODO: remove when deployment
markets = response.json()["events"][0]["markets"]
for i in range(len(markets)):
    print(markets[i]["id"])