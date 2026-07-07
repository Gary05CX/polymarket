import requests
import re
from pprint import pprint #TODO: remove when deployment
from datetime import datetime, timedelta

tomorrow = datetime.now() + timedelta(days = 1)
tmr_day = tomorrow.day
tmr_month = tomorrow.strftime("%B").lower()
tmr_year = tomorrow.year


url = "https://gamma-api.polymarket.com/events/keyset"

highest_or_lowest = "highest"

params = {
    "slug": f"{highest_or_lowest}-temperature-in-hong-kong-on-{tmr_month}-{tmr_day}-{tmr_year}", #TODO: package this with datetime lib
    "closed": False,
    "ascending":True
}

response = requests.request("GET", url, params=params)
pprint(response.json()) #TODO: remove when deployment
markets = response.json()["events"][0]["markets"]

market_ids = []
for i in range(len(markets)):
    market_ids.append(markets[i]["id"])
print(market_ids) #TODO: remove when deployment

url = f"https://gamma-api.polymarket.com/markets/{market_ids[0]}"

response = requests.request("GET", url)
pprint(response.json()) #TODO: remove when deployment
question = response.json()["question"]
degree = re.search(r"\d+", question)
under_or_over = re.search(r"(below|higher)", question)
if under_or_over:
    under_or_over = under_or_over.group()
else:
    under_or_over = None
print(degree.group()) #TODO: remove when deployment
print(under_or_over) #TODO: remove when deployment