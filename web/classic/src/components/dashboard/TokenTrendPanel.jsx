/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Card, RadioGroup, Radio } from '@douyinfe/semi-ui';
import { KeyRound } from 'lucide-react';
import { VChart } from '@visactor/react-vchart';

const TokenTrendPanel = ({
  spec_token_trend,
  tokenMetric,
  updateTokenMetric,
  tokenGranularity,
  updateTokenGranularity,
  CARD_PROPS,
  CHART_CONFIG,
  FLEX_CENTER_GAP2,
  t,
}) => {
  return (
    <Card
      {...CARD_PROPS}
      className='!rounded-2xl'
      title={
        <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between w-full gap-3'>
          <div className={FLEX_CENTER_GAP2}>
            <KeyRound size={16} />
            {t('按 Key 调用趋势')}
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <RadioGroup
              type='button'
              buttonSize='small'
              value={tokenGranularity}
              onChange={(e) => updateTokenGranularity(e.target.value)}
            >
              <Radio value='minute'>{t('分钟')}</Radio>
              <Radio value='hour'>{t('小时')}</Radio>
              <Radio value='day'>{t('天')}</Radio>
            </RadioGroup>
            <RadioGroup
              type='button'
              buttonSize='small'
              value={tokenMetric}
              onChange={(e) => updateTokenMetric(e.target.value)}
            >
              <Radio value='token_used'>{t('Token用量')}</Radio>
              <Radio value='count'>{t('调用次数')}</Radio>
              <Radio value='quota'>{t('花费')}</Radio>
            </RadioGroup>
          </div>
        </div>
      }
      bodyStyle={{ padding: 0 }}
    >
      <div className='h-96 p-2'>
        <VChart spec={spec_token_trend} option={CHART_CONFIG} />
      </div>
    </Card>
  );
};

export default TokenTrendPanel;
